// Package gitsync links a workspace to a git repo (GitHub or any remote,
// public or private) and pushes/pulls its skills as files.
//
// Repo layout (lossless — the same archive.Manifest format as Export):
//
//	<repo>/
//	  workspace.json                  # name, slug, description, color
//	  globals/manifest.json           # workspace-global skills
//	  globals/skills/<name>.md        # readable copies
//	  projects/<slug>/manifest.json   # one project + its skills
//	  projects/<slug>/skills/<name>.md
//
// Auth reuses your own git setup, so private repos just work:
//   - SSH remotes (git@github.com:…) use your keys/agent.
//   - HTTPS remotes can embed a token
//     (https://oauth2:TOKEN@github.com/owner/repo.git).
// SSH keys are recommended — tokens live in the local DB otherwise.
// Everything shells out to the `git` CLI with GIT_TERMINAL_PROMPT=0,
// so a missing credential fails fast with the remote's message instead
// of hanging on a password prompt.
package gitsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/model"
)

// WorkspaceFile is workspace.json at the repo root.
type WorkspaceFile struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	GitRemote   string `json:"gitRemote,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
}

// DirFor returns the default local checkout for a workspace slug:
// ~/.skillsmcp/workspaces/<slug>/repo (or SKILLSMCP_HOME equivalent).
func DirFor(appDir, slug string) string {
	return filepath.Join(appDir, "workspaces", slug, "repo")
}

func defaultTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 90*time.Second)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	combined := strings.TrimSpace(out.String() + "\n" + errb.String())
	combined = strings.TrimSpace(strings.TrimPrefix(combined, "\n"))
	if err != nil {
		if combined == "" {
			combined = err.Error()
		}
		return combined, fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(combined))
	}
	return out.String(), nil
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// CheckGit reports whether the git CLI is usable.
func CheckGit(ctx context.Context) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found (install git and sign in: SSH keys or gh auth)")
	}
	if _, err := run(ctx, "", "version"); err != nil {
		return fmt.Errorf("git is broken: %v", err)
	}
	return nil
}

// IsRepo reports whether dir is a git checkout.
func IsRepo(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && fi.IsDir()
}

// EnsureRepo creates the checkout when missing, points origin at the
// remote, and guarantees a commit identity so commits never fail on
// fresh machines.
func EnsureRepo(ctx context.Context, dir, remote, branch string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if !IsRepo(dir) {
		args := []string{"init"}
		if branch != "" {
			args = append(args, "-b", branch)
		}
		args = append(args, dir)
		// git init <dir> works from anywhere; run from parent for clarity.
		if _, err := run(ctx, filepath.Dir(dir), args...); err != nil {
			return err
		}
	}
	if _, err := run(ctx, dir, "remote", "get-url", "origin"); err != nil {
		if _, err := run(ctx, dir, "remote", "add", "origin", remote); err != nil {
			return err
		}
	} else if _, err := run(ctx, dir, "remote", "set-url", "origin", remote); err != nil {
		return err
	}
	// Local identity fallback (never touches global config).
	if out, _ := run(ctx, dir, "config", "user.name"); strings.TrimSpace(out) == "" {
		_, _ = run(ctx, dir, "config", "user.name", "SkillsMCP")
	}
	if out, _ := run(ctx, dir, "config", "user.email"); strings.TrimSpace(out) == "" {
		_, _ = run(ctx, dir, "config", "user.email", "skillsmcp@localhost")
	}
	return nil
}

// writeScope writes one manifest + its readable .md copies.
func writeScope(root string, m archive.Manifest) error {
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		return err
	}
	raw, err := m.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o644); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range m.Skills {
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		if err := os.WriteFile(filepath.Join(root, "skills", s.Name+".md"), []byte(archive.MarkdownFile(s)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// WriteWorkspace serializes the workspace into dir (exact rewrite of
// the skills tree; .git is never touched). ExportedAt is normalized
// away: it changes on every export and would otherwise make every push
// look dirty. Commit times already record when a sync happened.
func WriteWorkspace(dir string, ws model.Workspace, globals archive.Manifest, projects map[string]archive.Manifest) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	branch := ws.GitBranch
	if branch == "" {
		branch = "main"
	}
	wf := WorkspaceFile{
		Name: ws.Name, Slug: ws.Slug, Description: ws.Description,
		Color: ws.Color, GitRemote: ws.GitRemote, GitBranch: branch,
	}
	raw, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	// Exact rewrite: drop stale skill files first.
	_ = os.RemoveAll(filepath.Join(dir, "globals"))
	globals.ExportedAt = ""
	if err := writeScope(filepath.Join(dir, "globals"), globals); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(dir, "projects"))
	slugs := make([]string, 0, len(projects))
	for slug := range projects {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		m := projects[slug]
		m.ExportedAt = ""
		if err := writeScope(filepath.Join(dir, "projects", slug), m); err != nil {
			return err
		}
	}
	return nil
}

// ReadWorkspace parses a checkout back into manifests.
func ReadWorkspace(dir string) (WorkspaceFile, *archive.Manifest, map[string]archive.Manifest, error) {
	var wf WorkspaceFile
	raw, err := os.ReadFile(filepath.Join(dir, "workspace.json"))
	if err != nil {
		return wf, nil, nil, fmt.Errorf("not a skills workspace (no workspace.json): %v", err)
	}
	if err := json.Unmarshal(raw, &wf); err != nil {
		return wf, nil, nil, fmt.Errorf("bad workspace.json: %v", err)
	}
	var globals *archive.Manifest
	if graw, err := os.ReadFile(filepath.Join(dir, "globals", "manifest.json")); err == nil {
		if m, err := archive.Unmarshal(graw); err == nil {
			globals = &m
		} else {
			return wf, nil, nil, fmt.Errorf("bad globals/manifest.json: %v", err)
		}
	}
	out := map[string]archive.Manifest{}
	entries, _ := os.ReadDir(filepath.Join(dir, "projects"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "projects", e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		if m, err := archive.Unmarshal(raw); err == nil {
			out[e.Name()] = m
		} else {
			return wf, nil, nil, fmt.Errorf("bad projects/%s/manifest.json: %v", e.Name(), err)
		}
	}
	return wf, globals, out, nil
}

// Commit stages everything and commits when dirty. Returns false when
// there was nothing to commit.
func Commit(ctx context.Context, dir, msg string) (bool, error) {
	if _, err := run(ctx, dir, "add", "-A"); err != nil {
		return false, err
	}
	out, err := run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}
	if _, err := run(ctx, dir, "commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// gitPush pushes the branch to origin (sets upstream).
func gitPush(ctx context.Context, dir, remote, token, branch string) (string, error) {
	if branch == "" {
		branch = "main"
	}
	args := append(authArgs(remote, token), "push", "-u", "origin", branch)
	out, err := run(ctx, dir, args...)
	if err != nil {
		return out, err
	}
	return strings.TrimSpace(out), nil
}

// gitPull fast-forward pulls the branch from origin.
func gitPull(ctx context.Context, dir, remote, token, branch string) (string, error) {
	if branch == "" {
		branch = "main"
	}
	args := append(authArgs(remote, token), "pull", "--ff-only", "origin", branch)
	out, err := run(ctx, dir, args...)
	if err != nil {
		hint := firstLine(out)
		if strings.Contains(out, "no tracking information") || strings.Contains(out, "couldn't find remote ref") {
			return out, fmt.Errorf("nothing to pull yet (%s) — push first", hint)
		}
		return out, err
	}
	return strings.TrimSpace(out), nil
}

// Clone checks a remote out into dir (fresh dir or empty dir).
func Clone(ctx context.Context, remote, token, branch, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, remote, dir)
	args = append(authArgs(remote, token), args...)
	_, err := run(ctx, "", args...)
	return err
}

// authArgs injects a private-repo token for HTTPS remotes without
// persisting it anywhere: an AUTHORIZATION header (all-caps so git
// passes it through instead of overriding it) carrying a Bearer token.
// Works with GitHub/GitLab PATs. SSH and local remotes need nothing.
func authArgs(remote, token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	low := strings.ToLower(strings.TrimSpace(remote))
	if !strings.HasPrefix(low, "https://") {
		return nil
	}
	if strings.Contains(remote, "@") {
		return nil // credentials already embedded — leave them alone
	}
	return []string{"-c", "http.extraHeader=AUTHORIZATION: Bearer " + token}
}

// Status is a one-screen summary: branch, clean/dirty file count,
// ahead/behind vs upstream, and whether files differ from the DB.
// dbDirty is supplied by the caller (compare manifests) — empty = skip.
func Status(ctx context.Context, dir, dbDirty string) (string, error) {
	if !IsRepo(dir) {
		return "not linked yet — set a remote and Push to initialize", nil
	}
	branch := ""
	if out, err := run(ctx, dir, "branch", "--show-current"); err == nil {
		branch = strings.TrimSpace(out)
	}
	var b strings.Builder
	if branch != "" {
		fmt.Fprintf(&b, "branch %s", branch)
	}
	dirty := 0
	if out, err := run(ctx, dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		dirty = len(strings.Split(strings.TrimSpace(out), "\n"))
	}
	if dirty > 0 {
		fmt.Fprintf(&b, " · %d uncommitted file(s)", dirty)
	} else {
		b.WriteString(" · clean")
	}
	if out, err := run(ctx, dir, "rev-list", "--count", "--left-right", "@{u}...HEAD"); err == nil {
		parts := strings.Fields(out)
		if len(parts) == 2 {
			fmt.Fprintf(&b, " · ahead %s / behind %s", parts[1], parts[0])
		}
	}
	if dbDirty != "" {
		fmt.Fprintf(&b, "\n%s", dbDirty)
	}
	return b.String(), nil
}
