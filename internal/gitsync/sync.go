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
	Skills    int    `json:"skills"`
	Detail    string `json:"detail"`
}

// Push writes the workspace to its checkout, commits when dirty, and
// pushes to the linked remote. Main workspaces push too — linking a
// remote is optional per workspace, never required.
func Push(st *store.Store, wsID string) (PushResult, error) {
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
	out, err := gitPush(ctx, dir, w.GitRemote, token, w.GitBranch)
	if err != nil {
		if committed {
			res.Detail = fmt.Sprintf("committed locally, push failed: %v", err)
			return res, fmt.Errorf("committed locally, push failed: %v", err)
		}
		return res, err
	}
	res.Pushed = true
	res.Detail = strings.TrimSpace(out)
	if res.Detail == "" {
		res.Detail = fmt.Sprintf("pushed %d skill(s) to %s", res.Skills, RedactRemote(w.GitRemote))
	}
	return res, nil
}

// PullResult summarizes a pull.
type PullResult struct {
	Imported int      `json:"imported"`
	Skipped  []string `json:"skipped"`
	Projects []string `json:"projects"`
	Detail   string   `json:"detail"`
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
	if !IsRepo(dir) {
		if err := Clone(ctx, w.GitRemote, st.WorkspaceGitToken(w.ID), w.GitBranch, dir); err != nil {
			return res, fmt.Errorf("clone failed: %v", err)
		}
	} else {
		if err := EnsureRepo(ctx, dir, w.GitRemote, w.GitBranch); err != nil {
			return res, err
		}
		if _, err := gitPull(ctx, dir, w.GitRemote, st.WorkspaceGitToken(w.ID), w.GitBranch); err != nil {
			return res, err
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

// CloneWorkspace connects a (public or private) repo as a new workspace:
// clones, reads workspace.json, creates the workspace record linked to
// the remote, and restores every skill. Private repos work with your
// existing SSH keys/agent or a token (stored server-side, never shown).
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
		return model.Workspace{}, fmt.Errorf("clone failed (private repo? check SSH keys or token): %v", err)
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
func RedactRemote(remote string) string {
	if i := strings.Index(remote, "@"); i >= 0 {
		if j := strings.LastIndex(remote[:i], "//"); j >= 0 {
			return remote[:j+2] + "***@" + remote[i+1:]
		}
	}
	return remote
}
