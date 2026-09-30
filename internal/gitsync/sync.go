package gitsync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
)

// workspaceDir resolves the checkout dir for a workspace id.
func workspaceDir(st *store.Store, wsID string) (model.Workspace, string, error) {
	w, ok := st.GetWorkspace(wsID)
	if !ok {
		return w, "", fmt.Errorf("workspace not found")
	}
	return w, DirFor(store.AppDir(), w.Slug), nil
}

// PushResult summarizes a push.
type PushResult struct {
	Committed bool   `json:"committed"`
	Pushed    bool   `json:"pushed"`
	Forced    bool   `json:"forced"`
	Skills    int    `json:"skills"`
	Detail    string `json:"detail"`
	// Hint tells the user the one-click fix when the push fails
	// (force push, or pull/reset first). Empty on success.
	Hint string `json:"hint"`
}

// Push writes the workspace to its checkout, commits when dirty, and
// pushes to the linked remote. Main workspaces push too — linking a
// remote is optional per workspace, never required.
// force overwrites the remote (the escape hatch when the remote has
// commits you don't have, e.g. a README created on GitHub).
func Push(st *store.Store, wsID string, force bool) (PushResult, error) {
	var res PushResult
	w, dir, err := workspaceDir(st, wsID)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(w.GitRemote) == "" {
		return res, fmt.Errorf("workspace %q has no remote — link one first (set remote + branch)", w.Slug)
	}
	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		return res, err
	}
	if err := EnsureRepo(ctx, dir, w.GitRemote, w.GitBranch); err != nil {
		return res, err
	}
	globals, err := archive.Export(st, wsID, "global", "")
	if err != nil {
		return res, err
	}
	projs := map[string]archive.Manifest{}
	for _, p := range st.ListProjectsIn(wsID) {
		m, err := archive.Export(st, wsID, "project", p.ID)
		if err != nil {
			return res, err
		}
		projs[p.Slug] = m
	}
	if err := WriteWorkspace(dir, w, globals, projs); err != nil {
		return res, err
	}
	res.Skills = len(globals.Skills)
	for _, m := range projs {
		res.Skills += len(m.Skills)
	}
	committed, err := Commit(ctx, dir, fmt.Sprintf("skillsmcp sync %s (%d skills)", w.Slug, res.Skills))
	if err != nil {
		return res, err
	}
	res.Committed = committed
	token := st.WorkspaceGitToken(w.ID)
	out, err := gitPushForce(ctx, dir, w.GitRemote, token, w.GitBranch, force)
	if err != nil {
		clean := SanitizeOutput(out, w.GitRemote, token)
		res.Detail = capLines(clean, 25)
		res.Hint = pushHint(clean)
		if committed && !force {
			return res, fmt.Errorf("committed locally, push rejected: %s", firstLine(clean))
		}
		return res, fmt.Errorf("%s", firstLine(clean))
	}
	res.Pushed = true
	res.Forced = force
	res.Detail = strings.TrimSpace(out)
	if res.Detail == "" {
		res.Detail = fmt.Sprintf("pushed %d skill(s) to %s", res.Skills, RedactRemote(w.GitRemote))
	}
	return res, nil
}

// pushHint maps a push rejection to the one-click fix. The generated
// tree can't be merged file-by-file, so the choice is always: local
// wins (force push) or remote wins (pull / reset checkout to remote).
// Worded for any hosting platform (GitHub, GitLab, Bitbucket, Codeberg,
// self-hosted) — never assumes a provider.
func pushHint(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "non-fast-forward"),
		strings.Contains(low, "fetch first"),
		strings.Contains(low, "rejected"),
		strings.Contains(low, "unrelated histories"):
		return "The repo has commits you don't have (e.g. a README created on the hosting platform). Either Force push to overwrite it with your library, or open the Sync tab → Compare / Pull / Reset to take the repo's side first."
	case strings.Contains(low, "authentication failed"),
		strings.Contains(low, "could not read username"),
		strings.Contains(low, "permission denied"),
		strings.Contains(low, "403"), strings.Contains(low, "401"):
		return "The repo refused your credentials. If git on this machine is already authenticated (SSH keys/agent, credential manager, gh auth), no token is needed — just retry. Otherwise add an HTTPS token under Sync → Authentication (optional)."
	case strings.Contains(low, "could not resolve host"),
		strings.Contains(low, "network is unreachable"),
		strings.Contains(low, "connection timed out"):
		return "Network problem reaching the repo — check the URL and your connection."
	case strings.Contains(low, "src refspec") && strings.Contains(low, "does not match"):
		return "Nothing to push on that branch yet — this usually clears after the first successful push."
	default:
		return "Push failed — the full remote message is in Detail. Force push overwrites the repo; Reset checkout takes the repo's side."
	}
}

// capLines keeps the first n lines (full remote messages, not just line 1).
func capLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "… (truncated)")
	}
	return strings.Join(lines, "\n")
}

// RedactToken scrubs the stored token from command output, just in case
// a remote ever echoes it back.
func RedactToken(s, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

// PullResult summarizes a pull.
type PullResult struct {
	Imported int      `json:"imported"`
	Skipped  []string `json:"skipped"`
	Projects []string `json:"projects"`
	Detail   string   `json:"detail"`
	// Hint suggests the fix when the pull fails (reset checkout to remote).
	Hint string `json:"hint"`
}

// Pull fetches the linked remote into the checkout and restores every
// manifest into the workspace. Project manifests recreate missing
// projects; existing skill names are skipped.
func Pull(st *store.Store, wsID string) (PullResult, error) {
	var res PullResult
	res.Skipped = []string{}
	w, dir, err := workspaceDir(st, wsID)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(w.GitRemote) == "" {
		return res, fmt.Errorf("workspace %q has no remote — link one first", w.Slug)
	}
	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		return res, err
	}
	token := st.WorkspaceGitToken(w.ID)
	if !IsRepo(dir) {
		if err := Clone(ctx, w.GitRemote, token, w.GitBranch, dir); err != nil {
			return res, fmt.Errorf("clone failed: %v", SanitizeOutput(err.Error(), w.GitRemote, token))
		}
	} else {
		if err := EnsureRepo(ctx, dir, w.GitRemote, w.GitBranch); err != nil {
			return res, err
		}
		if out, err := gitPull(ctx, dir, w.GitRemote, token, w.GitBranch); err != nil {
			clean := SanitizeOutput(out, w.GitRemote, token)
			res.Hint = pullHint(clean)
			return res, fmt.Errorf("%s", firstLine(clean))
		}
	}
	_, globals, projects, err := ReadWorkspace(dir)
	if err != nil {
		return res, err
	}
	if globals != nil {
		r, err := archive.Restore(st, *globals, "global", "", wsID)
		if err != nil {
			return res, fmt.Errorf("restore globals: %v", err)
		}
		res.Imported += r.Imported
		res.Skipped = append(res.Skipped, r.Skipped...)
	}
	slugs := make([]string, 0, len(projects))
	for slug := range projects {
		slugs = append(slugs, slug)
	}
	for _, slug := range slugs {
		m := projects[slug]
		r, err := archive.Restore(st, m, "project", "", wsID)
		if err != nil {
			return res, fmt.Errorf("restore project %q: %v", slug, err)
		}
		res.Imported += r.Imported
		res.Skipped = append(res.Skipped, r.Skipped...)
		if r.Project != nil {
			res.Projects = append(res.Projects, r.Project.Slug)
		}
	}
	res.Detail = fmt.Sprintf("pulled %d skill(s) into %q (%d skipped as duplicates)", res.Imported, w.Slug, len(res.Skipped))
	return res, nil
}

// pullHint maps a pull failure to the one-click fix.
func pullHint(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "unrelated histories"),
		strings.Contains(low, "not possible to fast-forward"),
		strings.Contains(low, "divergent branches"),
		strings.Contains(low, "need to merge"):
		return "Local checkout and repo diverged (or never shared history, e.g. a README init). Either Reset checkout to take the repo's side (local-only DB skills are kept — restore skips existing names), or Force push to overwrite the repo with your library."
	case strings.Contains(low, "authentication failed"),
		strings.Contains(low, "could not read username"),
		strings.Contains(low, "permission denied"),
		strings.Contains(low, "403"), strings.Contains(low, "401"):
		return "The repo refused your credentials. If git on this machine is already authenticated (SSH keys/agent, credential manager, gh auth), no token is needed — just retry. Otherwise add an HTTPS token under Sync → Authentication (optional)."
	default:
		return "Pull failed — Reset checkout takes the repo's side (additive restore, existing names skipped)."
	}
}

// ResetResult summarizes a reset-to-remote.
type ResetResult struct {
	Imported int      `json:"imported"`
	Skipped  []string `json:"skipped"`
	Projects []string `json:"projects"`
	Detail   string   `json:"detail"`
}

// Reset discards the checkout state, takes the remote branch exactly,
// and restores it into the workspace (additive — existing skill names
// are skipped, so skills that only exist locally survive in the DB).
// This is the "remote wins" conflict escape hatch: one click, no merge.
func Reset(st *store.Store, wsID string) (ResetResult, error) {
	var res ResetResult
	res.Skipped = []string{}
	w, dir, err := workspaceDir(st, wsID)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(w.GitRemote) == "" {
		return res, fmt.Errorf("workspace %q has no remote — link one first", w.Slug)
	}
	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		return res, err
	}
	token := st.WorkspaceGitToken(w.ID)
	if !IsRepo(dir) {
		if err := Clone(ctx, w.GitRemote, token, w.GitBranch, dir); err != nil {
			return res, fmt.Errorf("clone failed: %v", SanitizeOutput(err.Error(), w.GitRemote, token))
		}
	} else {
		if err := EnsureRepo(ctx, dir, w.GitRemote, w.GitBranch); err != nil {
			return res, err
		}
		if out, err := resetToRemote(ctx, dir, w.GitRemote, token, w.GitBranch); err != nil {
			return res, fmt.Errorf("reset failed: %v", SanitizeOutput(firstLine(out+" "+err.Error()), w.GitRemote, token))
		}
	}
	_, globals, projects, err := ReadWorkspace(dir)
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "not a skills workspace") || strings.Contains(low, "no workspace.json") {
			// Foreign repo (e.g. a README init with no workspace files):
			// the reset already aligned local history with the remote,
			// so rewrite the checkout from the DB (unrelated files like
			// README are kept) — the next Push is then a fast-forward.
			dbGlobals, gerr := archive.Export(st, wsID, "global", "")
			if gerr != nil {
				return res, fmt.Errorf("reset to repo, re-export failed: %v", gerr)
			}
			dbProjs := map[string]archive.Manifest{}
			for _, p := range st.ListProjectsIn(wsID) {
				m, merr := archive.Export(st, wsID, "project", p.ID)
				if merr != nil {
					return res, fmt.Errorf("reset to repo, re-export failed: %v", merr)
				}
				dbProjs[p.Slug] = m
			}
			if werr := WriteWorkspace(dir, w, dbGlobals, dbProjs); werr != nil {
				return res, fmt.Errorf("reset to repo, checkout rewrite failed: %v", werr)
			}
			nSkills := len(dbGlobals.Skills)
			for _, m := range dbProjs {
				nSkills += len(m.Skills)
			}
			res.Detail = fmt.Sprintf("checkout reset to the repo (unrelated files like README kept); local library kept (%d skill(s)) — push to publish", nSkills)
			return res, nil
		}
		return res, err
	}
	if globals != nil {
		r, err := archive.Restore(st, *globals, "global", "", wsID)
		if err != nil {
			return res, fmt.Errorf("restore globals: %v", err)
		}
		res.Imported += r.Imported
		res.Skipped = append(res.Skipped, r.Skipped...)
	}
	for _, slug := range sortedKeys(projects) {
		m := projects[slug]
		r, err := archive.Restore(st, m, "project", "", wsID)
		if err != nil {
			return res, fmt.Errorf("restore project %q: %v", slug, err)
		}
		res.Imported += r.Imported
		res.Skipped = append(res.Skipped, r.Skipped...)
		if r.Project != nil {
			res.Projects = append(res.Projects, r.Project.Slug)
		}
	}
	res.Detail = fmt.Sprintf("checkout reset to the repo; restored %d skill(s) into %q (%d skipped as duplicates)", res.Imported, w.Slug, len(res.Skipped))
	return res, nil
}
// StatusLine reports branch/clean/ahead-behind plus whether the DB has
// unpublished changes (file tree vs live manifests).
func StatusLine(st *store.Store, wsID string) (string, error) {
	w, dir, err := workspaceDir(st, wsID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(w.GitRemote) == "" {
		n := st.CountWorkspaceSkills(wsID)
		return fmt.Sprintf("local only — no remote linked (%d skill(s)). Link a repo to sync.", n), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dbDirty := ""
	if IsRepo(dir) {
		if d, err := diffSummary(st, wsID, dir, w); err == nil && d != "" {
			dbDirty = d
		}
	}
	s, err := Status(ctx, dir, dbDirty)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s → %s (%s)", w.Slug, RedactRemote(w.GitRemote), s), nil
}

// diffSummary compares the live workspace against its checkout.
func diffSummary(st *store.Store, wsID, dir string, w model.Workspace) (string, error) {
	globals, err := archive.Export(st, wsID, "global", "")
	if err != nil {
		return "", err
	}
	projs := map[string]archive.Manifest{}
	for _, p := range st.ListProjectsIn(wsID) {
		m, err := archive.Export(st, wsID, "project", p.ID)
		if err != nil {
			return "", err
		}
		projs[p.Slug] = m
	}
	// Compare manifest-by-manifest against disk.
	changed := 0
	diskWf, diskGlobals, diskProjects, rerr := ReadWorkspace(dir)
	_ = diskWf
	if rerr != nil {
		return "checkout has no workspace files yet — push to publish", nil
	}
	if diskGlobals == nil || !manifestEqual(*diskGlobals, globals) {
		changed++
	}
	for slug, m := range projs {
		if dm, ok := diskProjects[slug]; !ok || !manifestEqual(dm, m) {
			changed++
		}
	}
	for slug := range diskProjects {
		if _, ok := projs[slug]; !ok {
			changed++
		}
	}
	if changed == 0 {
		return "in sync with checkout", nil
	}
	return fmt.Sprintf("%d section(s) differ from checkout — push to publish", changed), nil
}

func manifestEqual(a, b archive.Manifest) bool {
	if len(a.Skills) != len(b.Skills) {
		return false
	}
	for i := range a.Skills {
		as, bs := a.Skills[i], b.Skills[i]
		if as != bs {
			return false
		}
	}
	return true
}

// CloneWorkspace connects a repo on any hosting platform (public or
// private) as a new workspace: clones, reads workspace.json, creates the
// workspace record linked to the remote, and restores every skill. Private
// repos work with your existing git auth (SSH keys/agent, credential
// manager, gh auth) — no token needed when git itself is authenticated.
// An optional HTTPS token (stored locally, never shown, sent per-command
// as a header and never written into the repo) covers machines where git
// isn't set up yet.
func CloneWorkspace(st *store.Store, name, slug, remote, branch, token string) (model.Workspace, error) {
	slug = store.NormalizeSlug(slug)
	if slug == "" {
		slug = store.NormalizeSlug(name)
	}
	if err := store.ValidateWorkspace(name, slug); err != nil {
		return model.Workspace{}, err
	}
	if strings.TrimSpace(remote) == "" {
		return model.Workspace{}, fmt.Errorf("remote is required (SSH or HTTPS URL)")
	}
	dir := DirFor(store.AppDir(), slug)
	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		return model.Workspace{}, err
	}
	if IsRepo(dir) {
		// Checkout survived (e.g. the workspace was deleted but its repo
		// cache stayed): adopt it when it points at the same remote,
		// refuse when it belongs to another remote.
		if out, err := run(ctx, dir, "remote", "get-url", "origin"); err == nil {
			if strings.TrimSpace(out) != strings.TrimSpace(remote) {
				return model.Workspace{}, fmt.Errorf("a checkout for %q already points elsewhere — use another slug or remove %s", slug, dir)
			}
		}
	} else if err := Clone(ctx, remote, token, branch, dir); err != nil {
		return model.Workspace{}, fmt.Errorf("clone failed (private repo? make sure git on this machine is authenticated — SSH keys/agent, credential manager, gh auth — or add an HTTPS token): %v", SanitizeOutput(err.Error(), remote, token))
	}
	wf, globals, projects, err := ReadWorkspace(dir)
	if err != nil {
		return model.Workspace{}, err
	}
	if name == "" {
		name = wf.Name
	}
	if name == "" {
		name = slug
	}
	ws, err := st.CreateWorkspace(store.WorkspaceInput{
		Name: name, Slug: slug, Description: wf.Description, Color: wf.Color,
		GitRemote: remote, GitBranch: firstNonEmpty(branch, wf.GitBranch, "main"),
		GitToken: token,
	})
	if err != nil {
		return model.Workspace{}, err
	}
	if globals != nil {
		if _, err := archive.Restore(st, *globals, "global", "", ws.ID); err != nil {
			return ws, fmt.Errorf("workspace created, globals restore failed: %v", err)
		}
	}
	for _, slug := range sortedKeys(projects) {
		m := projects[slug]
		if _, err := archive.Restore(st, m, "project", "", ws.ID); err != nil {
			return ws, fmt.Errorf("workspace created, project %q restore failed: %v", slug, err)
		}
	}
	return ws, nil
}

func sortedKeys(m map[string]archive.Manifest) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// RedactRemote masks userinfo (tokens) in a remote URL for display in MCP
// responses and logs. Stored checkouts never contain the token (it travels
// per-command via header), but user-pasted URLs might.
// It delegates to SanitizeRemote, which parses the URL instead of regexing
// for ":" → "@", and works for any host (GitHub, GitLab, Bitbucket,
// Codeberg, self-hosted) plus SSH scp-like syntax and local paths.
func RedactRemote(remote string) string {
	disp, _ := SanitizeRemote(remote)
	return disp
}
