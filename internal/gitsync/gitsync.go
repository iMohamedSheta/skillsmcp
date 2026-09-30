// Package gitsync links a workspace to a git repo on any hosting platform
// (GitHub, GitLab, Bitbucket, Codeberg, self-hosted, or a local path;
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
// Auth reuses your own git setup, so private repos just work with no token
// in SkillsMCP when git itself is authenticated:
//   - SSH remotes (git@host:org/repo.git, ssh://git@host/...) use your
//     keys/agent.
//   - HTTPS remotes use your system's credential helper (Git Credential
//     Manager, osxkeychain, libsecret, wincred, cache, store) or gh auth.
// An optional HTTPS token (Sync → Authentication) covers machines where
// git isn't authenticated yet: it is stored locally, never shown, never
// written into the repo, and sent per-command as an AUTHORIZATION header
// (never embedded in the URL — embedding passwords in URLs is insecure
// because they leak into logs, prompts, and error output).
// Everything shells out to the `git` CLI with GIT_TERMINAL_PROMPT=0,
// so a missing credential fails fast with the remote's message instead
// of hanging on a password prompt. Errors and log lines are sanitized:
// tokens and embedded URL userinfo are masked before display.
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
	"sync"
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

// run executes git and returns stdout on success. On failure it returns the
// combined output for Detail surfaces — or err.Error() when git printed
// nothing. Callers testing for "unset/empty" MUST check err too: a bare
// `out == ""` check never matches on failure (it sees "exit status 1").
func run(ctx context.Context, dir string, args ...string) (string, error) {
	exe, err := gitExe()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	// GIT_TERMINAL_PROMPT=0 fails fast instead of hanging on a password
	// prompt. System credential helpers (Git Credential Manager, osxkeychain,
	// libsecret, wincred, cache, store) and SSH agents still work — only
	// interactive terminal prompts are disabled. So private repos work with
	// no token in SkillsMCP as long as git itself is authenticated.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	combined := strings.TrimSpace(out.String() + "\n" + errb.String())
	combined = strings.TrimSpace(strings.TrimPrefix(combined, "\n"))
	if err != nil {
		if combined == "" {
			combined = err.Error()
		}
		// Never leak the per-command Bearer token into the error: it
		// travels in argv, so join through the masking helper.
		return combined, fmt.Errorf("git %s: %s", safeJoinArgs(args), firstLine(combined))
	}
	return out.String(), nil
}

// gitCache remembers the verified git binary for the process lifetime.
var gitCache struct {
	sync.Mutex
	exe string
	ok  bool
}

// gitExe returns a WORKING git binary. PATH can hold a broken shim first
// (e.g. Laragon's msys launcher printing "BUG (fork bomb)"), while a good
// Git for Windows sits later in PATH — exec.LookPath alone would stick us
// with the broken one, so every candidate is probed with `version` and the
// first one that actually runs wins (cached).
func gitExe() (string, error) {
	gitCache.Lock()
	defer gitCache.Unlock()
	if gitCache.ok {
		return gitCache.exe, nil
	}
	var tried []string
	for _, c := range gitCandidates() {
		tried = append(tried, c)
		if probeGit(c) {
			gitCache.exe = c
			gitCache.ok = true
			return c, nil
		}
	}
	hint := ""
	if len(tried) > 0 {
		hint = fmt.Sprintf(" (tried %s — each failed; reinstall Git for Windows and make sure its cmd/ dir is on PATH)", strings.Join(tried, ", "))
	}
	return "", fmt.Errorf("no working git found%s (install git and sign in: SSH keys or gh auth)", hint)
}

// probeGit reports whether exe runs `version` successfully.
func probeGit(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version")
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "git version")
}

// gitCandidates lists every git binary on PATH in order, plus well-known
// install locations (GUI apps often inherit a minimal PATH).
func gitCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	names := []string{"git.exe", "git", "git.cmd", "git.bat"}
	for _, dir := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, n := range names {
			full := filepath.Join(dir, n)
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				add(full)
			}
		}
	}
	// Bare `git` via LookPath (covers PATHEXT quirks) as a last PATH resort.
	if p, err := exec.LookPath("git"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			add(abs)
		} else {
			add(p)
		}
	}
	// Well-known Windows install spots.
	progFiles := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("LocalAppData")}
	for _, base := range progFiles {
		if strings.TrimSpace(base) == "" {
			continue
		}
		for _, sub := range []string{`Git\cmd\git.exe`, `Git\bin\git.exe`, `Programs\Git\cmd\git.exe`} {
			full := filepath.Join(base, sub)
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				add(full)
			}
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// CheckGit reports whether a working git CLI is usable.
func CheckGit(ctx context.Context) error {
	exe, err := gitExe()
	if err != nil {
		return err
	}
	if _, err := run(ctx, "", "version"); err != nil {
		// The cached binary broke mid-session (uninstalled?): drop the
		// cache so the next call re-probes remaining candidates.
		gitCache.Lock()
		gitCache.ok = false
		gitCache.exe = ""
		gitCache.Unlock()
		if exe2, err2 := gitExe(); err2 == nil {
			_ = exe2
			if _, err3 := run(ctx, "", "version"); err3 == nil {
				return nil
			}
		}
		return fmt.Errorf("git is broken (%s): %v", exe, err)
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
	// NOTE: test the error as well as the output — run() substitutes
	// err.Error() for empty output on failure, so a bare `out == ""`
	// check never sees "unset" (it sees "exit status 1" instead) and
	// the fallback silently never fires on machines without any git
	// identity configured.
	if missingIdentity(ctx, dir) {
		_, _ = run(ctx, dir, "config", "user.name", "SkillsMCP")
		_, _ = run(ctx, dir, "config", "user.email", "skillsmcp@localhost")
	}
	return nil
}

// missingIdentity reports whether git can find no author identity at any
// config scope, i.e. a commit would fail with "Author identity unknown".
func missingIdentity(ctx context.Context, dir string) bool {
	if out, err := run(ctx, dir, "config", "user.name"); err != nil || strings.TrimSpace(out) == "" {
		return true
	}
	if out, err := run(ctx, dir, "config", "user.email"); err != nil || strings.TrimSpace(out) == "" {
		return true
	}
	return false
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
// there was nothing to commit. The author's own git identity is preferred;
// on machines with none configured the app identity is injected per-command
// (-c never touches any config file), so sync commits never fail with
// "Author identity unknown" on fresh machines.
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
	args := []string{"commit", "-m", msg}
	if missingIdentity(ctx, dir) {
		args = append([]string{"-c", "user.name=SkillsMCP", "-c", "user.email=skillsmcp@localhost"}, args...)
	}
	if _, err := run(ctx, dir, args...); err != nil {
		return false, err
	}
	return true, nil
}

// gitPushForce pushes, optionally with --force (overwrite the remote).
// Force is the conflict escape hatch for a generated tree: the files are
// rewritten from the database on every push, so merging remote edits
// file-by-file is meaningless — either the local library wins (force)
// or the remote does (reset + pull).
func gitPushForce(ctx context.Context, dir, remote, token, branch string, force bool) (string, error) {
	if branch == "" {
		branch = "main"
	}
	args := append(authArgs(remote, token), "push", "-u", "origin", branch)
	if force {
		args = append(authArgs(remote, token), "push", "--force", "-u", "origin", branch)
	}
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

// resetToRemote discards the checkout state and makes it exactly the
// remote branch, then the caller restores the files into the database
// (additively — existing skill names are skipped, so local-only skills
// survive). This is the "remote wins" conflict escape hatch.
func resetToRemote(ctx context.Context, dir, remote, token, branch string) (string, error) {
	if branch == "" {
		branch = "main"
	}
	auth := authArgs(remote, token)
	if _, err := run(ctx, dir, append(append([]string{}, auth...), "fetch", "origin", branch)...); err != nil {
		return "", err
	}
	out, err := run(ctx, dir, "reset", "--hard", "origin/"+branch)
	if err != nil {
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

// authArgs injects an optional HTTPS token without persisting it anywhere:
// an AUTHORIZATION header (all-caps so git passes it through instead of
// overriding it) carrying a Bearer token. Works with any hosting platform
// (GitHub, GitLab, Bitbucket, Codeberg, self-hosted). SSH, local paths, and
// plain http(s) without a token need nothing — the system's git auth
// (SSH keys/agent, Git Credential Manager, gh auth, etc.) handles them.
// A token is only needed for private HTTPS repos on machines where git
// itself isn't authenticated yet.
func authArgs(remote, token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	low := strings.ToLower(strings.TrimSpace(remote))
	if !strings.HasPrefix(low, "https://") {
		return nil
	}
	// A password already embedded in the URL: leave it alone rather than
	// stacking a second credential. (A bare username is fine — the header
	// still applies.)
	if RemoteHasPassword(remote) {
		return nil
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
