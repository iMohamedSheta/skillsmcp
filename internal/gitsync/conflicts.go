// Conflict resolution engine: compare the app library (SQLite) against
// the linked repo's files, skill by skill and file by file, so the UI can
// show every part and its change and let the user merge with our editor.
//
// Model:
//   LOCAL  = live DB (what the app shows, edited with SkillDetail /
//            MarkdownEditor).
//   REMOTE = origin/<branch> manifests (what `git show` reads after fetch,
//            never touching the working tree).
// The checkout working tree is generated from the DB on push, so a
// two-way LOCAL vs REMOTE compare is the whole story: a skill only on one
// side was added (or deleted) there; a skill on both sides with different
// fields was edited on one or both sides.
//
// After the user picks per-skill winners (or edits a merged version in
// our editor), Resolve applies the picks to the DB and rewrites the
// checkout files so `git status` shows exactly what will be pushed.
// Pushing afterwards still needs --force when histories diverged, because
// the merged library overwrites the repo tree (skills are lossless,
// unrelated files like a README init are called out explicitly).
package gitsync

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/store"
)

// SkillResolution is one row of the user's merge decision, sent by the UI.
type SkillResolution struct {
	Name        string               `json:"name"`
	Scope       string               `json:"scope"`       // global|project (as shown)
	ProjectSlug string               `json:"projectSlug"` // "" for globals
	Action      string               `json:"action"`      // keep-local|take-remote|merged
	Merged      *archive.SkillEntry  `json:"merged,omitempty"`
}

// ProjectResolution resolves a project-meta conflict (name/desc/color).
type ProjectResolution struct {
	Slug   string               `json:"slug"`
	Action string               `json:"action"` // keep-local|take-remote|merged
	Merged *archive.ProjectMeta `json:"merged,omitempty"`
}

// SkillConflict is one skill on either side (or both) with its field diff
// and the repo file that carries it.
type SkillConflict struct {
	Name          string               `json:"name"`
	Scope         string               `json:"scope"`
	ProjectSlug   string               `json:"projectSlug"`
	Kind          string               `json:"kind"` // added-local|added-remote|modified|unchanged
	ChangedFields []string             `json:"changedFields"`
	Local         *archive.SkillEntry  `json:"local,omitempty"`
	Remote        *archive.SkillEntry  `json:"remote,omitempty"`
	LocalFile     string               `json:"localFile"`
	FileStatus    string               `json:"fileStatus"` // A|M|D|=
	Detail        string               `json:"detail"`
}

// FileChange is one repo path with its sync status. Covers every part:
// workspace.json, every manifest.json, every skills/*.md, plus any other
// file the repo carries (e.g. a README init).
type FileChange struct {
	Path        string `json:"path"`
	Status      string `json:"status"` // added-local|added-remote|modified|deleted-remote|unchanged|other-remote
	Kind        string `json:"kind"`   // skill|manifest|workspace|other
	SkillName   string `json:"skillName,omitempty"`
	Scope       string `json:"scope,omitempty"`
	ProjectSlug string `json:"projectSlug,omitempty"`
	Detail      string `json:"detail"`
}

// ProjectConflict compares one project's meta (name/description/color).
type ProjectConflict struct {
	Slug          string               `json:"slug"`
	Kind          string               `json:"kind"`
	ChangedFields []string             `json:"changedFields"`
	Local         *archive.ProjectMeta `json:"local,omitempty"`
	Remote        *archive.ProjectMeta `json:"remote,omitempty"`
}

// ConflictsResult is the full compare: every skill, every project meta,
// workspace.json, and every file.
type ConflictsResult struct {
	Workspace     string            `json:"workspace"`
	Branch        string            `json:"branch"`
	Remote        string            `json:"remote"`
	Fetched       bool              `json:"fetched"`
	HasConflicts  bool              `json:"hasConflicts"`
	LocalSkills   int               `json:"localSkills"`
	RemoteSkills  int               `json:"remoteSkills"`
	AddedLocal    int               `json:"addedLocal"`
	AddedRemote   int               `json:"addedRemote"`
	Modified      int               `json:"modified"`
	Unchanged     int               `json:"unchanged"`
	Skills        []SkillConflict   `json:"skills"`
	Files         []FileChange      `json:"files"`
	Projects      []ProjectConflict `json:"projects"`
	WorkspaceLoc  WorkspaceFile     `json:"workspaceLocal"`
	WorkspaceRem  *WorkspaceFile    `json:"workspaceRemote,omitempty"`
	WsChanged     []string          `json:"workspaceChangedFields"`
	OtherFiles    []string          `json:"otherFiles"`
	Detail        string            `json:"detail"`
	Hint          string            `json:"hint,omitempty"`
}

// ResolveRequest is the UI payload: one decision per conflicted part.
// WorkspaceAction is keep-local|take-remote|merged (empty = keep-local);
// WorkspaceMerged carries the edited workspace meta for merged.
type ResolveRequest struct {
	Skills          []SkillResolution   `json:"skills"`
	Projects        []ProjectResolution `json:"projects"`
	WorkspaceAction string              `json:"workspaceAction"`
	WorkspaceMerged *WorkspaceFile      `json:"workspaceMerged,omitempty"`
}

// ResolveResult summarizes applying the user's picks to the DB.
type ResolveResult struct {
	Updated   int      `json:"updated"`
	Created   int      `json:"created"`
	Deleted   int      `json:"deleted"`
	Skipped   int      `json:"skipped"`
	Projects  int      `json:"projects"`
	Detail    string   `json:"detail"`
	FilesNote string   `json:"filesNote"`
}

func skillKey(scope, projectSlug, name string) string {
	return strings.ToLower(scope) + "|" + strings.ToLower(projectSlug) + "|" + strings.ToLower(strings.TrimSpace(name))
}

func skillFile(scope, projectSlug, name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.ToLower(scope) == "project" && projectSlug != "" {
		return "projects/" + strings.ToLower(projectSlug) + "/skills/" + n + ".md"
	}
	return "globals/skills/" + n + ".md"
}

// compareEntries diffs two skill entries (SortOrder ignored: manual order
// is not a content conflict).
func compareEntries(a, b archive.SkillEntry) []string {
	var out []string
	if strings.TrimSpace(a.Description) != strings.TrimSpace(b.Description) {
		out = append(out, "description")
	}
	if a.Content != b.Content {
		out = append(out, "content")
	}
	if strings.TrimSpace(a.Category) != strings.TrimSpace(b.Category) {
		out = append(out, "category")
	}
	if strings.TrimSpace(a.Tags) != strings.TrimSpace(b.Tags) {
		out = append(out, "tags")
	}
	if a.Enabled != b.Enabled {
		out = append(out, "enabled")
	}
	return out
}

func compareProjectMeta(a, b archive.ProjectMeta) []string {
	var out []string
	if strings.TrimSpace(a.Name) != strings.TrimSpace(b.Name) {
		out = append(out, "name")
	}
	if strings.TrimSpace(a.Description) != strings.TrimSpace(b.Description) {
		out = append(out, "description")
	}
	if strings.TrimSpace(a.Color) != strings.TrimSpace(b.Color) {
		out = append(out, "color")
	}
	return out
}

// fetchRemoteManifests fetches origin/<branch> and reads its manifests via
// `git show` (working tree untouched). Returns remote globals (nil when the
// repo has no workspace files yet), projects by slug, workspace.json, the
// full remote file list, and the fetch output.
func fetchRemoteManifests(ctx context.Context, dir, remote, token, branch string) (*archive.Manifest, map[string]archive.Manifest, *WorkspaceFile, []string, string, error) {
	if branch == "" {
		branch = "main"
	}
	auth := authArgs(remote, token)
	if _, err := run(ctx, dir, append(append([]string{}, auth...), "fetch", "origin", branch)...); err != nil {
		return nil, nil, nil, nil, "", err
	}
	ref := "origin/" + branch
	lsOut, err := run(ctx, dir, "ls-tree", "-r", "--name-only", ref)
	if err != nil {
		// No such ref yet (empty remote): treat as no remote content.
		return nil, map[string]archive.Manifest{}, nil, nil, "", fmt.Errorf("nothing on the remote yet (%s)", firstLine(lsOut))
	}
	var files []string
	for _, ln := range strings.Split(strings.TrimSpace(lsOut), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			files = append(files, ln)
		}
	}
	show := func(path string) ([]byte, bool) {
		// `git show` prints the blob to stdout; run() returns stdout only.
		out, err := run(ctx, dir, "show", ref+":"+path)
		if err != nil {
			return nil, false
		}
		return []byte(out), true
	}
	var globals *archive.Manifest
	projects := map[string]archive.Manifest{}
	var wf *WorkspaceFile
	if raw, ok := show("workspace.json"); ok {
		var parsed WorkspaceFile
		if err := json.Unmarshal(raw, &parsed); err == nil {
			wf = &parsed
		}
	}
	if raw, ok := show("globals/manifest.json"); ok {
		if m, err := archive.Unmarshal(raw); err == nil {
			globals = &m
		} else {
			return nil, nil, nil, files, "", fmt.Errorf("bad globals/manifest.json on remote: %v", err)
		}
	}
	// Discover project slugs from the remote tree + try each manifest.
	slugs := map[string]bool{}
	for _, f := range files {
		if strings.HasPrefix(f, "projects/") && strings.HasSuffix(f, "/manifest.json") {
			parts := strings.Split(f, "/")
			if len(parts) == 3 {
				slugs[parts[1]] = true
			}
		}
	}
	for slug := range slugs {
		if raw, ok := show("projects/" + slug + "/manifest.json"); ok {
			if m, err := archive.Unmarshal(raw); err == nil {
				projects[slug] = m
			} else {
				return nil, nil, nil, files, "", fmt.Errorf("bad projects/%s/manifest.json on remote: %v", slug, err)
			}
		}
	}
	return globals, projects, wf, files, "", nil
}

// GetConflicts compares the workspace DB against its remote repo.
func GetConflicts(st *store.Store, wsID string) (ConflictsResult, error) {
	var res ConflictsResult
	res.Skills = []SkillConflict{}
	res.Files = []FileChange{}
	res.Projects = []ProjectConflict{}
	res.OtherFiles = []string{}

	w, ok := st.GetWorkspace(wsID)
	if !ok {
		return res, fmt.Errorf("workspace not found")
	}
	res.Workspace = w.Slug
	branch := w.GitBranch
	if branch == "" {
		branch = "main"
	}
	res.Branch = branch
	res.Remote = RedactRemote(w.GitRemote)
	if strings.TrimSpace(w.GitRemote) == "" {
		return res, fmt.Errorf("workspace %q has no remote — link one first", w.Slug)
	}

	// LOCAL: export live DB.
	localGlobals, err := archive.Export(st, wsID, "global", "")
	if err != nil {
		return res, err
	}
	localProjects := map[string]archive.Manifest{}
	localProjMeta := map[string]archive.ProjectMeta{}
	for _, p := range st.ListProjectsIn(wsID) {
		m, err := archive.Export(st, wsID, "project", p.ID)
		if err != nil {
			return res, err
		}
		localProjects[p.Slug] = m
		localProjMeta[p.Slug] = archive.ProjectMeta{Name: p.Name, Slug: p.Slug, Description: p.Description, Color: p.Color}
	}
	res.WorkspaceLoc = WorkspaceFile{Name: w.Name, Slug: w.Slug, Description: w.Description, Color: w.Color, GitBranch: branch}

	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		return res, err
	}
	dir := DirFor(store.AppDir(), w.Slug)
	if err := EnsureRepo(ctx, dir, w.GitRemote, branch); err != nil {
		return res, err
	}
	token := st.WorkspaceGitToken(w.ID)

	remoteGlobals, remoteProjects, remoteWf, remoteFiles, _, ferr := fetchRemoteManifests(ctx, dir, w.GitRemote, token, branch)
	if ferr != nil {
		low := strings.ToLower(ferr.Error())
		if strings.Contains(low, "nothing on the remote yet") || strings.Contains(low, "couldn't find remote ref") || strings.Contains(low, "no such remote") {
			// Empty remote: everything local is added-local, no remote side.
			res.Fetched = true
			res.Detail = "Remote branch has nothing yet — every local skill is new. Push to publish."
			buildLocalOnlyResult(&res, localGlobals, localProjects, localProjMeta)
			return res, nil
		}
		// Auth/network failure: surface the sanitized message + fix.
		// Sanitize first so a credential-bearing URL echoed by git never
		// reaches the UI, MCP responses, or the log file.
		serr := SanitizeOutput(ferr.Error(), w.GitRemote, token)
		hint := "Fetch failed — check the URL, branch, and that git on this machine is authenticated (SSH keys/agent, credential manager, gh auth). An optional HTTPS token under Sync → Authentication covers machines where git isn't set up."
		ll := strings.ToLower(serr)
		if strings.Contains(ll, "authentication") || strings.Contains(ll, "permission") || strings.Contains(ll, "401") || strings.Contains(ll, "403") {
			hint = "The repo refused your credentials. If git on this machine is already authenticated, no token is needed — just retry. Otherwise add an HTTPS token under Sync → Authentication (optional)."
		}
		res.Hint = hint
		return res, fmt.Errorf("%s", firstLine(serr))
	}
	res.Fetched = true
	if remoteWf != nil {
		res.WorkspaceRem = remoteWf
		res.WsChanged = compareWorkspaceFile(res.WorkspaceLoc, *remoteWf)
	}

	// Index entries by key.
	type entry struct {
		scope string
		slug  string
		e     archive.SkillEntry
	}
	local := map[string]entry{}
	for _, e := range localGlobals.Skills {
		local[skillKey("global", "", e.Name)] = entry{"global", "", e}
	}
	for slug, m := range localProjects {
		for _, e := range m.Skills {
			local[skillKey("project", slug, e.Name)] = entry{"project", slug, e}
		}
	}
	remote := map[string]entry{}
	if remoteGlobals != nil {
		for _, e := range remoteGlobals.Skills {
			remote[skillKey("global", "", e.Name)] = entry{"global", "", e}
		}
	}
	for slug, m := range remoteProjects {
		for _, e := range m.Skills {
			remote[skillKey("project", slug, e.Name)] = entry{"project", slug, e}
		}
	}
	res.LocalSkills = len(local)
	res.RemoteSkills = len(remote)

	// Union of keys, sorted for a stable UI.
	keys := map[string]bool{}
	for k := range local {
		keys[k] = true
	}
	for k := range remote {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	// Track per-scope file dirtiness for manifest entries.
	globalDirty := false
	projDirty := map[string]bool{}

	for _, k := range sorted {
		l, hasL := local[k]
		r, hasR := remote[k]
		scope, slug, name := splitKey(k, l, r)
		file := skillFile(scope, slug, name)
		var sc SkillConflict
		sc.Scope = scope
		sc.ProjectSlug = slug
		sc.LocalFile = file
		sc.ChangedFields = []string{}
		switch {
		case hasL && !hasR:
			sc.Kind = "added-local"
			sc.FileStatus = "A"
			e := l.e
			sc.Local = &e
			sc.Name = e.Name
			sc.Detail = "Only in your app — push to publish it."
			res.AddedLocal++
			markDirty(scope, slug, &globalDirty, projDirty)
		case !hasL && hasR:
			sc.Kind = "added-remote"
			sc.FileStatus = "A"
			e := r.e
			sc.Remote = &e
			sc.Name = e.Name
			sc.Detail = "Only in the repo — pull it into your app or ignore it."
			res.AddedRemote++
			markDirty(scope, slug, &globalDirty, projDirty)
		default:
			le, re := l.e, r.e
			sc.Name = le.Name
			sc.Local = &le
			sc.Remote = &re
			if diff := compareEntries(le, re); len(diff) > 0 {
				sc.Kind = "modified"
				sc.ChangedFields = diff
				sc.FileStatus = "M"
				sc.Detail = "Changed on both sides in: " + strings.Join(diff, ", ") + "."
				res.Modified++
				markDirty(scope, slug, &globalDirty, projDirty)
			} else {
				sc.Kind = "unchanged"
				sc.FileStatus = "="
				sc.Detail = "Same in your app and the repo."
				res.Unchanged++
			}
		}
		res.Skills = append(res.Skills, sc)
	}
	res.HasConflicts = res.AddedLocal+res.AddedRemote+res.Modified > 0

	// Project metas: union of slugs.
	pslugs := map[string]bool{}
	for s := range localProjects {
		pslugs[s] = true
	}
	for s := range remoteProjects {
		pslugs[s] = true
	}
	var psorted []string
	for s := range pslugs {
		psorted = append(psorted, s)
	}
	sort.Strings(psorted)
	for _, slug := range psorted {
		lm, hasL := localProjMeta[slug]
		var rm *archive.ProjectMeta
		hasR := false
		if m, ok := remoteProjects[slug]; ok {
			hasR = true
			meta := archive.ProjectMeta{Name: "", Slug: slug}
			if m.Project != nil {
				meta = *m.Project
			} else {
				meta.Slug = slug
			}
			rm = &meta
		}
		var pc ProjectConflict
		pc.Slug = slug
		switch {
		case hasL && !hasR:
			pc.Kind = "added-local"
			c := lm
			pc.Local = &c
		case !hasL && hasR:
			pc.Kind = "added-remote"
			pc.Remote = rm
		default:
			c := lm
			pc.Local = &c
			pc.Remote = rm
			if diff := compareProjectMeta(lm, *rm); len(diff) > 0 {
				pc.Kind = "modified"
				pc.ChangedFields = diff
			} else {
				pc.Kind = "unchanged"
			}
		}
		if pc.Kind != "unchanged" {
			res.HasConflicts = true
		}
		res.Projects = append(res.Projects, pc)
	}

	// Files: every part.
	res.Files = append(res.Files, FileChange{
		Path: "workspace.json", Status: wsFileStatus(res.WsChanged),
		Kind: "workspace", Detail: wsFileDetail(res.WsChanged),
	})
	if globalDirty {
		res.Files = append(res.Files, FileChange{Path: "globals/manifest.json", Status: "modified", Kind: "manifest", Scope: "global", Detail: "Global skills changed — manifest differs."})
	} else {
		res.Files = append(res.Files, FileChange{Path: "globals/manifest.json", Status: "unchanged", Kind: "manifest", Scope: "global", Detail: "In sync."})
	}
	for _, slug := range psorted {
		st := "unchanged"
		d := "In sync."
		if projDirty[slug] {
			st = "modified"
			d = "Project skills changed — manifest differs."
		}
		// Added-side projects have no manifest on one side.
		if _, ok := localProjects[slug]; !ok {
			st = "added-remote"
			d = "Project only in the repo."
		} else if _, ok := remoteProjects[slug]; !ok {
			st = "added-local"
			d = "Project only in your app."
		}
		res.Files = append(res.Files, FileChange{Path: "projects/" + slug + "/manifest.json", Status: st, Kind: "manifest", ProjectSlug: slug, Detail: d})
	}
	for _, sc := range res.Skills {
		fstatus := "unchanged"
		detail := "In sync."
		switch sc.Kind {
		case "added-local":
			fstatus = "added-local"
			detail = sc.Name + " only in your app — " + sc.LocalFile + " will be created on push."
		case "added-remote":
			fstatus = "added-remote"
			detail = sc.Name + " only in the repo — import it or ignore it."
		case "modified":
			fstatus = "modified"
			detail = sc.Name + " differs in: " + strings.Join(sc.ChangedFields, ", ") + "."
		}
		res.Files = append(res.Files, FileChange{
			Path: sc.LocalFile, Status: fstatus, Kind: "skill",
			SkillName: sc.Name, Scope: sc.Scope, ProjectSlug: sc.ProjectSlug, Detail: detail,
		})
	}
	// Other repo files (README init etc.): anything not in the known set.
	known := map[string]bool{"workspace.json": true, "globals/manifest.json": true}
	for _, sc := range res.Skills {
		known[sc.LocalFile] = true
	}
	for _, slug := range psorted {
		known["projects/"+slug+"/manifest.json"] = true
		// Remote .md files are the skill files above; anything else is other.
	}
	for _, f := range remoteFiles {
		if known[f] {
			continue
		}
		if strings.HasPrefix(f, "globals/skills/") || strings.HasPrefix(f, "projects/") {
			// A .md without a manifest entry (hand-edited repo): still a skill file.
			known[f] = true
			res.Files = append(res.Files, FileChange{Path: f, Status: "added-remote", Kind: "skill", Detail: "File in repo with no manifest entry — hand-edited or removed skill."})
			continue
		}
		res.OtherFiles = append(res.OtherFiles, f)
		res.Files = append(res.Files, FileChange{Path: f, Status: "other-remote", Kind: "other", Detail: "Unrelated repo file (e.g. README init). Force push overwrites it; normal push is rejected until histories converge."})
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })

	if !res.HasConflicts && len(res.OtherFiles) == 0 && len(res.WsChanged) == 0 {
		res.Detail = fmt.Sprintf("In sync — %d skill(s) identical on both sides.", res.Unchanged)
	} else {
		parts := []string{}
		if res.AddedLocal > 0 {
			parts = append(parts, fmt.Sprintf("%d only in app", res.AddedLocal))
		}
		if res.AddedRemote > 0 {
			parts = append(parts, fmt.Sprintf("%d only in repo", res.AddedRemote))
		}
		if res.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d changed on both sides", res.Modified))
		}
		if res.Unchanged > 0 {
			parts = append(parts, fmt.Sprintf("%d identical", res.Unchanged))
		}
		res.Detail = strings.Join(parts, " · ") + "."
		if len(res.OtherFiles) > 0 {
			res.Detail += fmt.Sprintf(" Plus %d unrelated repo file(s) (%s).", len(res.OtherFiles), strings.Join(res.OtherFiles, ", "))
		}
		res.Hint = "Pick Keep mine / Take theirs per skill, or edit a merged version in the editor, then Apply + push (force when histories diverged)."
	}
	return res, nil
}

func buildLocalOnlyResult(res *ConflictsResult, globals archive.Manifest, projects map[string]archive.Manifest, meta map[string]archive.ProjectMeta) {
	for _, e := range globals.Skills {
		ec := e
		res.Skills = append(res.Skills, SkillConflict{
			Name: e.Name, Scope: "global", Kind: "added-local",
			Local: &ec, LocalFile: skillFile("global", "", e.Name),
			FileStatus: "A", Detail: "Only in your app — push to publish it.",
		})
		res.Files = append(res.Files, FileChange{Path: skillFile("global", "", e.Name), Status: "added-local", Kind: "skill", SkillName: e.Name, Scope: "global", Detail: "New skill — file will be created on push."})
	}
	var slugs []string
	for s := range projects {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		for _, e := range projects[slug].Skills {
			ec := e
			res.Skills = append(res.Skills, SkillConflict{
				Name: e.Name, Scope: "project", ProjectSlug: slug, Kind: "added-local",
				Local: &ec, LocalFile: skillFile("project", slug, e.Name),
				FileStatus: "A", Detail: "Only in your app — push to publish it.",
			})
			res.Files = append(res.Files, FileChange{Path: skillFile("project", slug, e.Name), Status: "added-local", Kind: "skill", SkillName: e.Name, Scope: "project", ProjectSlug: slug, Detail: "New skill — file will be created on push."})
		}
		if m, ok := meta[slug]; ok {
			mc := m
			res.Projects = append(res.Projects, ProjectConflict{Slug: slug, Kind: "added-local", Local: &mc})
		}
	}
	res.LocalSkills = len(res.Skills)
	res.AddedLocal = len(res.Skills)
	res.HasConflicts = len(res.Skills) > 0
	res.Files = append([]FileChange{
		{Path: "workspace.json", Status: "added-local", Kind: "workspace", Detail: "Workspace not on the remote yet."},
		{Path: "globals/manifest.json", Status: "added-local", Kind: "manifest", Scope: "global", Detail: "New on push."},
	}, res.Files...)
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
}

func splitKey(k string, l, r struct {
	scope string
	slug  string
	e     archive.SkillEntry
},
) (scope, slug, name string) {
	// Prefer the local side's casing for display; fall back to remote.
	if l.e.Name != "" {
		return l.scope, l.slug, l.e.Name
	}
	return r.scope, r.slug, r.e.Name
}

func markDirty(scope, slug string, globalDirty *bool, projDirty map[string]bool) {
	if scope == "project" {
		projDirty[slug] = true
	} else {
		*globalDirty = true
	}
}

func compareWorkspaceFile(local, remote WorkspaceFile) []string {
	var out []string
	if strings.TrimSpace(local.Name) != strings.TrimSpace(remote.Name) {
		out = append(out, "name")
	}
	if strings.TrimSpace(local.Description) != strings.TrimSpace(remote.Description) {
		out = append(out, "description")
	}
	if strings.TrimSpace(local.Color) != strings.TrimSpace(remote.Color) {
		out = append(out, "color")
	}
	return out
}

func wsFileStatus(changed []string) string {
	if len(changed) == 0 {
		return "unchanged"
	}
	return "modified"
}

func wsFileDetail(changed []string) string {
	if len(changed) == 0 {
		return "In sync."
	}
	return "Workspace meta differs in: " + strings.Join(changed, ", ") + "."
}

// Resolve applies the user's per-skill (and project) picks to the DB and
// rewrites the checkout files so git status shows what a push will do.
func Resolve(st *store.Store, wsID string, skills []SkillResolution, projects []ProjectResolution, workspaceAction string, workspaceMerged *WorkspaceFile) (ResolveResult, error) {
	var res ResolveResult
	w, ok := st.GetWorkspace(wsID)
	if !ok {
		return res, fmt.Errorf("workspace not found")
	}
	// Re-read conflicts once for remote contents (single fetch).
	conf, err := GetConflicts(st, wsID)
	if err != nil {
		// Empty-remote case still resolves trivially (nothing remote to take).
		if strings.Contains(strings.ToLower(err.Error()), "no remote") {
			return res, err
		}
		// If fetch failed for auth reasons, still allow keep-local applies?
		// No — the user can't see the remote side, so refuse.
		return res, err
	}
	remoteByKey := map[string]*archive.SkillEntry{}
	remoteScope := map[string]string{}
	remoteProj := map[string]string{}
	for _, sc := range conf.Skills {
		if sc.Remote != nil {
			k := skillKey(sc.Scope, sc.ProjectSlug, sc.Name)
			remoteByKey[k] = sc.Remote
			remoteScope[k] = sc.Scope
			remoteProj[k] = sc.ProjectSlug
		}
	}
	remoteProjMeta := map[string]*archive.ProjectMeta{}
	for _, pc := range conf.Projects {
		if pc.Remote != nil {
			remoteProjMeta[strings.ToLower(pc.Slug)] = pc.Remote
		}
	}

	byReq := map[string]SkillResolution{}
	for _, r := range skills {
		byReq[skillKey(r.Scope, r.ProjectSlug, r.Name)] = r
	}
	// Default: every conflicted skill without an explicit pick keeps local.
	for _, sc := range conf.Skills {
		k := skillKey(sc.Scope, sc.ProjectSlug, sc.Name)
		if _, ok := byReq[k]; !ok && sc.Kind != "unchanged" {
			byReq[k] = SkillResolution{Name: sc.Name, Scope: sc.Scope, ProjectSlug: sc.ProjectSlug, Action: "keep-local"}
		}
	}

	for key, req := range byReq {
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if action == "" {
			action = "keep-local"
		}
		name := strings.ToLower(strings.TrimSpace(req.Name))
		if name == "" {
			// Key fallback: skillKey is scope|slug|name.
			if parts := strings.Split(key, "|"); len(parts) == 3 {
				name = parts[2]
			}
		}
		remote := remoteByKey[key]
		scope := req.Scope
		projSlug := strings.ToLower(strings.TrimSpace(req.ProjectSlug))
		if scope == "" {
			scope = remoteScope[key]
		}
		if projSlug == "" {
			projSlug = remoteProj[key]
		}
		localSkill, hasLocal := st.GetSkillIn(wsID, name)

		applyEntry := func(e archive.SkillEntry, targetScope, targetProj string) error {
			targetScope = strings.ToLower(strings.TrimSpace(targetScope))
			if targetScope != "project" {
				targetScope = "global"
				targetProj = ""
			}
			var pid string
			if targetScope == "project" {
				p, ok := st.GetProjectBySlugIn(wsID, targetProj)
				if !ok {
					// Recreate from remote meta when available.
					meta := remoteProjMeta[strings.ToLower(targetProj)]
					in := store.ProjectInput{Name: targetProj, Slug: targetProj, WorkspaceID: wsID}
					if meta != nil {
						in.Name = meta.Name
						in.Description = meta.Description
						in.Color = meta.Color
					}
					if in.Name == "" {
						in.Name = targetProj
					}
					created, err := st.CreateProject(in)
					if err != nil {
						return fmt.Errorf("recreate project %q: %v", targetProj, err)
					}
					pid = created.ID
					res.Projects++
				} else {
					pid = p.ID
				}
			}
			in := store.SkillInput{
				Name: e.Name, Description: e.Description, Content: e.Content,
				Category: e.Category, Tags: e.Tags,
				Scope: targetScope, ProjectID: pid, WorkspaceID: wsID, Enabled: e.Enabled,
			}
			if hasLocal {
				if _, err := st.UpdateSkill(localSkill.ID, in); err != nil {
					return err
				}
				res.Updated++
			} else {
				if _, err := st.CreateSkill(in); err != nil {
					// Duplicate race: count as skipped.
					res.Skipped++
					return nil
				}
				res.Created++
			}
			return nil
		}

		switch action {
		case "keep-local", "keep", "mine", "local":
			res.Skipped++
			continue
		case "take-remote", "take", "theirs", "remote":
			if remote == nil {
				// Remote deleted (or never had it) → drop the local copy.
				if hasLocal {
					if err := st.DeleteSkill(localSkill.ID); err != nil {
						return res, fmt.Errorf("delete %q: %v", name, err)
					}
					res.Deleted++
				} else {
					res.Skipped++
				}
				continue
			}
			if err := applyEntry(*remote, scope, projSlug); err != nil {
				return res, fmt.Errorf("take-remote %q: %v", name, err)
			}
		case "merged", "merge", "edit":
			if req.Merged == nil {
				return res, fmt.Errorf("skill %q: merged action needs the edited skill", name)
			}
			m := *req.Merged
			if strings.TrimSpace(m.Name) == "" {
				m.Name = name
			}
			if err := store.ValidateSkill(strings.ToLower(strings.TrimSpace(m.Name)), m.Description, m.Content); err != nil {
				return res, fmt.Errorf("merged %q invalid: %v", name, err)
			}
			useScope, useProj := scope, projSlug
			if useScope == "" {
				useScope = "global"
			}
			if err := applyEntry(m, useScope, useProj); err != nil {
				return res, fmt.Errorf("merged %q: %v", name, err)
			}
		case "delete":
			if hasLocal {
				if err := st.DeleteSkill(localSkill.ID); err != nil {
					return res, err
				}
				res.Deleted++
			} else {
				res.Skipped++
			}
		default:
			return res, fmt.Errorf("skill %q: unknown action %q (want keep-local|take-remote|merged)", name, req.Action)
		}
	}

	// Projects.
	for _, pr := range projects {
		action := strings.ToLower(strings.TrimSpace(pr.Action))
		if action == "" {
			action = "keep-local"
		}
		slug := strings.ToLower(strings.TrimSpace(pr.Slug))
		local, hasLocal := st.GetProjectBySlugIn(wsID, slug)
		remote := remoteProjMeta[slug]
		switch action {
		case "keep-local", "keep", "mine", "local":
			continue
		case "take-remote", "take", "theirs", "remote":
			if remote == nil {
				continue
			}
			if hasLocal {
				if _, err := st.UpdateProject(local.ID, store.ProjectInput{Name: remote.Name, Slug: remote.Slug, Description: remote.Description, Color: remote.Color, WorkspaceID: wsID}); err != nil {
					return res, err
				}
				res.Projects++
			} else {
				if _, err := st.CreateProject(store.ProjectInput{Name: remote.Name, Slug: remote.Slug, Description: remote.Description, Color: remote.Color, WorkspaceID: wsID}); err != nil {
					return res, err
				}
				res.Projects++
			}
		case "merged", "merge":
			if pr.Merged == nil {
				return res, fmt.Errorf("project %q: merged action needs the edited meta", slug)
			}
			if hasLocal {
				if _, err := st.UpdateProject(local.ID, store.ProjectInput{Name: pr.Merged.Name, Slug: pr.Merged.Slug, Description: pr.Merged.Description, Color: pr.Merged.Color, WorkspaceID: wsID}); err != nil {
					return res, err
				}
				res.Projects++
			} else {
				if _, err := st.CreateProject(store.ProjectInput{Name: pr.Merged.Name, Slug: pr.Merged.Slug, Description: pr.Merged.Description, Color: pr.Merged.Color, WorkspaceID: wsID}); err != nil {
					return res, err
				}
				res.Projects++
			}
		}
	}

	// Workspace meta.
	if strings.ToLower(strings.TrimSpace(workspaceAction)) == "take-remote" && conf.WorkspaceRem != nil {
		r := conf.WorkspaceRem
		cur, _ := st.GetWorkspace(wsID)
		if _, err := st.UpdateWorkspace(wsID, store.WorkspaceInput{Name: r.Name, Slug: cur.Slug, Description: r.Description, Color: r.Color, GitRemote: cur.GitRemote, GitBranch: cur.GitBranch}); err != nil {
			return res, err
		}
	} else if strings.ToLower(strings.TrimSpace(workspaceAction)) == "merged" && workspaceMerged != nil {
		cur, _ := st.GetWorkspace(wsID)
		if _, err := st.UpdateWorkspace(wsID, store.WorkspaceInput{Name: workspaceMerged.Name, Slug: cur.Slug, Description: workspaceMerged.Description, Color: workspaceMerged.Color, GitRemote: cur.GitRemote, GitBranch: cur.GitBranch}); err != nil {
			return res, err
		}
	}

	// Rewrite the checkout files so the Files view + git status match the
	// resolved app state (no commit — Push commits).
	updated, _ := st.GetWorkspace(wsID)
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
	dir := DirFor(store.AppDir(), updated.Slug)
	// Dir follows the stored slug (matches Push); renames keep paying
	// attention to the current slug's checkout.
	if err := WriteWorkspace(dir, updated, globals, projs); err != nil {
		return res, fmt.Errorf("wrote app state but checkout rewrite failed: %v", err)
	}
	nSkills := len(globals.Skills)
	for _, m := range projs {
		nSkills += len(m.Skills)
	}
	res.Detail = fmt.Sprintf("Applied to your app: %d updated, %d imported from repo, %d removed, %d kept (%d project meta). Checkout files rewritten (%d skills) — review, then Push (force when histories diverged).", res.Updated, res.Created, res.Deleted, res.Skipped, res.Projects, nSkills)
	res.FilesNote = fmt.Sprintf("Checkout at %s now mirrors your app: globals/manifest.json + globals/skills/*.md + projects/*/manifest.json + projects/*/skills/*.md rewritten; unrelated repo files untouched.", dir)
	_ = w
	return res, nil
}
