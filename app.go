package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"skillsmcp/internal/applog"
	"skillsmcp/internal/archive"
	"skillsmcp/internal/gitsync"
	"skillsmcp/internal/mcpserver"
	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
	"skillsmcp/internal/version"
)

// App is the Wails binding surface. The frontend edits skills + projects;
// the AI only ever reads enabled skills as MCP tools (global or per-project).
type App struct {
	ctx     context.Context
	store   *store.Store
	mcp     *mcpserver.Server
	mcpAddr string
	dbPath  string
}

func NewApp(dbPath string) *App {
	return &App{dbPath: dbPath}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	applog.Init(store.AppDir())
	s, err := store.Open(store.ResolveDBPath(a.dbPath))
	if err != nil {
		applog.Error("store.init", err)
		panic(fmt.Sprintf("failed to open database: %v", err))
	}
	a.store = s
	a.mcp = mcpserver.New(s)
	// auto-start MAIN MCP on loopback so opencode works out of the box
	if addr, err := a.mcp.Start(9423); err == nil {
		a.mcpAddr = addr
		applog.Info("mcp listening at %s", addr)
	} else {
		applog.Error("mcp.start", err)
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.mcp != nil {
		a.mcp.Stop()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
}

// Version returns the baked-in release tag ("dev" for local builds).
func (a *App) Version() string { return version.Version }

// StorePath shows where skills.db lives.
func (a *App) StorePath() string {
	if a.store != nil {
		return a.store.Path()
	}
	return store.ResolveDBPath(a.dbPath)
}

func (a *App) GetDBPath() string { return a.StorePath() }

// ---------- skills CRUD (Wails-bound; MCP reads the same store) ----------

func (a *App) ListSkills() []model.Skill {
	out := a.store.ListSkills(true)
	if out == nil {
		return []model.Skill{}
	}
	return out
}

// ListSkillsIn lists one workspace's skills (the UI calls this for the
// active workspace).
func (a *App) ListSkillsIn(workspaceID string) []model.Skill {
	out := a.store.ListSkillsIn(workspaceID, true)
	if out == nil {
		return []model.Skill{}
	}
	return out
}

func (a *App) GetSkill(name string) (model.Skill, error) {
	sk, ok := a.store.GetSkill(name)
	if !ok {
		return model.Skill{}, fmt.Errorf("unknown skill %q", name)
	}
	return sk, nil
}

func (a *App) GetSkillByID(id string) (model.Skill, error) {
	sk, ok := a.store.GetSkillByID(id)
	if !ok {
		return model.Skill{}, fmt.Errorf("skill not found")
	}
	return sk, nil
}

func (a *App) CreateSkill(in store.SkillInput) (model.Skill, error) {
	return a.store.CreateSkill(in)
}

func (a *App) UpdateSkill(id string, in store.SkillInput) (model.Skill, error) {
	return a.store.UpdateSkill(id, in)
}

func (a *App) DeleteSkill(id string) error {
	return a.store.DeleteSkill(id)
}

func (a *App) SetSkillEnabled(id string, enabled bool) (model.Skill, error) {
	return a.store.SetEnabled(id, enabled)
}

// MoveSkill relocates a skill between Global and a project (sidebar drag-drop).
func (a *App) MoveSkill(id string, scope string, projectID string) (model.Skill, error) {
	return a.store.MoveSkill(id, scope, projectID)
}

// MoveSkillTo relocates a skill, optionally across workspaces.
func (a *App) MoveSkillTo(id string, scope string, projectID string, workspaceID string) (model.Skill, error) {
	return a.store.MoveSkillTo(id, scope, projectID, workspaceID)
}

// ReorderSkills persists a manual card order (home grid drag-drop).
func (a *App) ReorderSkills(ids []string) error {
	return a.store.ReorderSkills(ids)
}

func (a *App) NormalizeName(raw string) string {
	return store.NormalizeName(raw)
}

// ---------- projects CRUD ----------

func (a *App) ListProjects() []model.Project {
	out := a.store.ListProjects()
	if out == nil {
		return []model.Project{}
	}
	return out
}

// ListProjectsIn lists one workspace's projects.
func (a *App) ListProjectsIn(workspaceID string) []model.Project {
	out := a.store.ListProjectsIn(workspaceID)
	if out == nil {
		return []model.Project{}
	}
	return out
}

func (a *App) CreateProject(in store.ProjectInput) (model.Project, error) {
	return a.store.CreateProject(in)
}

func (a *App) UpdateProject(id string, in store.ProjectInput) (model.Project, error) {
	return a.store.UpdateProject(id, in)
}

func (a *App) DeleteProject(id string) error {
	return a.store.DeleteProject(id)
}

func (a *App) NormalizeSlug(raw string) string {
	return store.NormalizeSlug(raw)
}

// ProjectSkillCount reports how many skills belong to a project.
func (a *App) ProjectSkillCount(id string) int {
	return a.store.CountProjectSkills(id)
}

// ---------- workspaces (each: globals + projects + own git repo + MCPs) ----------

func (a *App) ListWorkspaces() []model.Workspace {
	out := a.store.ListWorkspaces()
	if out == nil {
		return []model.Workspace{}
	}
	return out
}

func (a *App) GetMainWorkspace() (model.Workspace, error) {
	if w, ok := a.store.GetMainWorkspace(); ok {
		return w, nil
	}
	return model.Workspace{}, fmt.Errorf("no main workspace")
}

func (a *App) CreateWorkspace(in store.WorkspaceInput) (model.Workspace, error) {
	return a.store.CreateWorkspace(in)
}

func (a *App) UpdateWorkspace(id string, in store.WorkspaceInput) (model.Workspace, error) {
	return a.store.UpdateWorkspace(id, in)
}

func (a *App) DeleteWorkspace(id string) error {
	return a.store.DeleteWorkspace(id)
}

// SetWorkspaceGit links a workspace to a git remote on any host (empty
// remote unlinks and drops the token). No token is needed when git on this
// machine is already authenticated; token "": keep stored (dropped when
// remote changes).
func (a *App) SetWorkspaceGit(id string, remote string, branch string, token string) (model.Workspace, error) {
	return a.store.SetWorkspaceGit(id, remote, branch, token)
}

// WorkspaceGitStatus is the one-line sync state (branch, clean/dirty,
// ahead/behind, unpublished changes).
func (a *App) WorkspaceGitStatus(id string) string {
	s, err := gitsync.StatusLine(a.store, id)
	if err != nil {
		return "git status failed: " + err.Error()
	}
	return s
}

// PushWorkspace writes the workspace to its checkout, commits when dirty,
// and pushes to the linked remote. force overwrites the remote (the fix
// when the repo has commits you don't have, e.g. a README init).
func (a *App) PushWorkspace(id string, force bool) map[string]any {
	res, err := gitsync.Push(a.store, id, force)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "detail": res.Detail, "hint": res.Hint, "committed": res.Committed}
	}
	return map[string]any{"ok": true, "committed": res.Committed, "pushed": res.Pushed, "forced": res.Forced, "skills": res.Skills, "detail": res.Detail}
}

// PullWorkspace fetches the linked remote and restores skills into the workspace.
func (a *App) PullWorkspace(id string) map[string]any {
	res, err := gitsync.Pull(a.store, id)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "hint": res.Hint}
	}
	skipped := res.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	return map[string]any{"ok": true, "imported": res.Imported, "skipped": skipped, "projects": res.Projects, "detail": res.Detail}
}

// ResetWorkspace discards the checkout, takes the remote branch exactly,
// and restores it additively (existing names skipped — local-only skills
// survive). The one-click "remote wins" conflict fix.
func (a *App) ResetWorkspace(id string) map[string]any {
	res, err := gitsync.Reset(a.store, id)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	skipped := res.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	return map[string]any{"ok": true, "imported": res.Imported, "skipped": skipped, "projects": res.Projects, "detail": res.Detail}
}

// GetWorkspaceConflicts compares the app library against the linked repo
// and returns every part with its change: every skill (local vs remote
// entry + changed fields), every project meta, workspace.json, and every
// file (manifests, skills/*.md, unrelated repo files like README init).
// The UI renders this with our editor for per-skill merges.
func (a *App) GetWorkspaceConflicts(id string) (gitsync.ConflictsResult, error) {
	return gitsync.GetConflicts(a.store, id)
}

// ResolveWorkspaceConflicts applies the user's per-part picks to the DB
// (keep-local = do nothing, take-remote = overwrite/import, merged =
// apply the editor's merged entry) and rewrites the checkout files so git
// status shows exactly what a push will publish.
func (a *App) ResolveWorkspaceConflicts(id string, req gitsync.ResolveRequest) (gitsync.ResolveResult, error) {
	return gitsync.Resolve(a.store, id, req.Skills, req.Projects, req.WorkspaceAction, req.WorkspaceMerged)
}

// CloneWorkspace connects a repo on any host (public or private — system
// git auth such as SSH keys/agent, credential manager, gh auth; optional
// token, stored server-side and never shown) as a new workspace.
func (a *App) CloneWorkspace(name string, slug string, remote string, branch string, token string) (model.Workspace, error) {
	return gitsync.CloneWorkspace(a.store, name, slug, remote, branch, token)
}

// WorkspaceSkillCount reports globals + project skills in a workspace.
func (a *App) WorkspaceSkillCount(id string) int {
	return a.store.CountWorkspaceSkills(id)
}

// ---------- archive export / import (lossless .zip backups) ----------

// ExportArchive builds a .zip of one workspace's global skills
// (scope="global") or one project's skills + project meta
// (scope="project", projectID set). Empty workspaceID = main workspace.
// Returns filename + base64 zip; the UI turns it into a download.
func (a *App) ExportArchive(scope string, projectID string, workspaceID string) (map[string]string, error) {
	m, err := archive.Export(a.store, workspaceID, scope, projectID)
	if err != nil {
		return nil, err
	}
	zb, err := archive.Build(m)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := base64.NewEncoder(base64.StdEncoding, &buf)
	if _, err := enc.Write(zb); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return map[string]string{"filename": m.FileName(), "base64": buf.String(), "skills": fmt.Sprint(len(m.Skills))}, nil
}

// ImportArchive restores a .zip built by ExportArchive (base64).
// Empty scope/project/workspace = the archive remembers (project
// archives recreate their project when the slug is gone). Existing
// names are skipped, never overwritten.
func (a *App) ImportArchive(base64zip string, targetScope string, targetProjectID string, targetWorkspaceID string) (map[string]any, error) {
	zb, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64zip))
	if err != nil {
		// Also accept a raw data-URL (frontend convenience).
		if i := strings.Index(base64zip, "base64,"); i >= 0 {
			zb, err = base64.StdEncoding.DecodeString(base64zip[i+len("base64,"):])
		}
		if err != nil {
			return nil, fmt.Errorf("not valid base64: %v", err)
		}
	}
	m, err := archive.Parse(zb)
	if err != nil {
		return nil, err
	}
	res, err := archive.Restore(a.store, m, targetScope, targetProjectID, targetWorkspaceID)
	if err != nil {
		return nil, err
	}
	skipped := res.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	out := map[string]any{"imported": res.Imported, "skipped": skipped, "scope": m.Scope}
	if res.Project != nil {
		out["project"] = res.Project.Slug
	} else if m.Project != nil {
		out["project"] = m.Project.Slug
	}
	return out, nil
}

// ---------- settings / logs ----------

func (a *App) GetSettings() map[string]string { return a.store.GetSettings() }

func (a *App) SetSetting(key, value string) {
	if key == "" {
		return
	}
	a.store.SetSetting(key, value)
}

func (a *App) GetLogs(n int) []string {
	if n <= 0 || n > 500 {
		n = 200
	}
	return applog.Tail(n)
}

func (a *App) LogPath() string { return applog.Path() }
func (a *App) ClearLogs()      { _ = applog.Clear() }

// ---------- MCP ----------

func (a *App) MCPStatus() map[string]any {
	return map[string]any{"running": a.mcp.Running(), "url": a.mcpAddr}
}

func (a *App) StartMCP() string {
	if addr, err := a.mcp.Start(9423); err == nil {
		a.mcpAddr = addr
	}
	return a.mcpAddr
}

// MCPToolsPreview lists the MAIN (global) MCP tools.
func (a *App) MCPToolsPreview() string {
	skills := a.store.ListGlobalSkills(false)
	names := []string{"list_skills", "get_skill(name)", "list_projects", "list_project_skills(project)"}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, " · ")
}

// ControlMCPToolsPreview lists the management MCP tools.
func (a *App) ControlMCPToolsPreview() string {
	ctl := mcpserver.NewControl(a.store)
	names := []string{}
	for _, t := range ctl.ToolNames() {
		names = append(names, t)
	}
	return strings.Join(names, " · ")
}

// ProjectMCPToolsPreview lists one project's MCP tools (globals + project).
func (a *App) ProjectMCPToolsPreview(slug string) string {
	p, ok := a.store.GetProjectBySlug(slug)
	if !ok {
		return "unknown project"
	}
	skills := a.store.ListProjectMCPSkills(p.ID, false)
	names := []string{"list_skills", "get_skill(name)"}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, " · ")
}

// WorkspaceMCPToolsPreview lists one workspace's main MCP tools.
func (a *App) WorkspaceMCPToolsPreview(workspaceID string) string {
	ws, ok := a.store.GetWorkspace(workspaceID)
	if !ok {
		return "unknown workspace"
	}
	skills := a.store.ListGlobalSkillsIn(ws.ID, false)
	names := []string{"list_skills", "get_skill(name)", "list_projects", "list_project_skills(project)"}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, " · ")
}

// ProjectMCPToolsPreviewIn lists a workspace's project MCP tools.
func (a *App) ProjectMCPToolsPreviewIn(workspaceID string, slug string) string {
	p, ok := a.store.GetProjectBySlugIn(workspaceID, slug)
	if !ok {
		return "unknown project"
	}
	skills := a.store.ListProjectMCPSkills(p.ID, false)
	names := []string{"list_skills", "get_skill(name)"}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, " · ")
}

// exePath resolves this binary's path for MCP stdio configs.
func exePath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		if goruntime.GOOS == "windows" {
			return "SkillsMCP.exe"
		}
		return "SkillsMCP"
	}
	return exe
}

func (a *App) BinaryName() string {
	if goruntime.GOOS == "windows" {
		return "SkillsMCP.exe"
	}
	return "SkillsMCP"
}

// OpencodeConfig returns copy-paste JSON for opencode.json — MAIN global MCP.
func (a *App) OpencodeConfig() string {
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "skillsmcp": {
      "type": "local",
      "command": ["%s", "mcp"],
      "enabled": true
    }
  }
}`, esc)
}

// OpencodeConfigForProject returns the per-project MCP block for opencode.json.
// Install it ALONGSIDE the main block under a different key (skillsmcp-<slug>).
func (a *App) OpencodeConfigForProject(slug string) string {
	p, ok := a.store.GetProjectBySlug(slug)
	if !ok {
		return "{} // unknown project " + slug
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "skillsmcp-%s": {
      "type": "local",
      "command": ["%s", "mcp", "--project", "%s"],
      "enabled": true
    }
  }
}`, p.Slug, esc, p.Slug)
}

// workspaceMCPKey is the opencode key for a workspace MCP: skillsmcp for
// main, skillsmcp-<slug> otherwise.
func (a *App) workspaceMCPKey(workspaceID string) (key string, args []string, ok bool) {
	w, found := a.store.GetWorkspace(workspaceID)
	if !found {
		return "", nil, false
	}
	if w.IsMain {
		return "skillsmcp", []string{"mcp"}, true
	}
	return "skillsmcp-" + w.Slug, []string{"mcp", "--workspace", w.Slug}, true
}

// OpencodeConfigForWorkspace returns a workspace's main MCP block.
func (a *App) OpencodeConfigForWorkspace(workspaceID string) string {
	key, args, ok := a.workspaceMCPKey(workspaceID)
	if !ok {
		return "{} // unknown workspace"
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	cmd, _ := json.Marshal(append([]string{esc}, args...))
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "%s": {
      "type": "local",
      "command": %s,
      "enabled": true
    }
  }
}`, key, cmd)
}

// OpencodeConfigForProjectIn returns a workspace's project MCP block
// (skillsmcp-<slug> for main, skillsmcp-<ws>-<slug> otherwise).
func (a *App) OpencodeConfigForProjectIn(workspaceID string, slug string) string {
	w, found := a.store.GetWorkspace(workspaceID)
	if !found {
		return "{} // unknown workspace"
	}
	p, ok := a.store.GetProjectBySlugIn(w.ID, slug)
	if !ok {
		return "{} // unknown project " + slug
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	key, args := "skillsmcp-"+p.Slug, []string{"mcp", "--project", p.Slug}
	if !w.IsMain {
		key = "skillsmcp-" + w.Slug + "-" + p.Slug
		args = []string{"mcp", "--workspace", w.Slug, "--project", p.Slug}
	}
	cmd, _ := json.Marshal(append([]string{esc}, args...))
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "%s": {
      "type": "local",
      "command": %s,
      "enabled": true
    }
  }
}`, key, cmd)
}

// OpencodeConfigControl returns the management MCP block for opencode.json.
// Install it ALONGSIDE the main block under the skillsmcp-control key.
func (a *App) OpencodeConfigControl() string {
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "skillsmcp-control": {
      "type": "local",
      "command": ["%s", "mcp", "--control"],
      "enabled": true
    }
  }
}`, esc)
}
// ClaudeConfig returns the copy-paste stdio JSON for Claude Desktop / Cursor / Windsurf.
func (a *App) ClaudeConfig() string {
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "mcpServers": {
    "skillsmcp": {
      "command": "%s",
      "args": ["mcp"],
      "description": "SkillsMCP global — start with list_skills, then call the matching skill tool."
    }
  }
}`, esc)
}

// ClaudeConfigForProject returns the per-project stdio block (own MCP per project).
func (a *App) ClaudeConfigForProject(slug string) string {
	p, ok := a.store.GetProjectBySlug(slug)
	if !ok {
		return "{} // unknown project " + slug
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "mcpServers": {
    "skillsmcp-%s": {
      "command": "%s",
      "args": ["mcp", "--project", "%s"],
      "description": "SkillsMCP project %s — globals + project skills. Start with list_skills."
    }
  }
}`, p.Slug, esc, p.Slug, p.Slug)
}

// ClaudeConfigForWorkspace returns a workspace's main MCP stdio block.
func (a *App) ClaudeConfigForWorkspace(workspaceID string) string {
	key, args, ok := a.workspaceMCPKey(workspaceID)
	if !ok {
		return "{} // unknown workspace"
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	argJSON, _ := json.Marshal(args)
	w, _ := a.store.GetWorkspace(workspaceID)
	desc := "SkillsMCP " + w.Slug + " workspace — start with list_skills, then call the matching skill tool."
	if w.IsMain {
		desc = "SkillsMCP global — start with list_skills, then call the matching skill tool."
	}
	return fmt.Sprintf(`{
  "mcpServers": {
    "%s": {
      "command": "%s",
      "args": %s,
      "description": "%s"
    }
  }
}`, key, esc, argJSON, desc)
}

// ClaudeConfigForProjectIn returns a workspace's project MCP stdio block.
func (a *App) ClaudeConfigForProjectIn(workspaceID string, slug string) string {
	w, found := a.store.GetWorkspace(workspaceID)
	if !found {
		return "{} // unknown workspace"
	}
	p, ok := a.store.GetProjectBySlugIn(w.ID, slug)
	if !ok {
		return "{} // unknown project " + slug
	}
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	key, args := "skillsmcp-"+p.Slug, []string{"mcp", "--project", p.Slug}
	if !w.IsMain {
		key = "skillsmcp-" + w.Slug + "-" + p.Slug
		args = []string{"mcp", "--workspace", w.Slug, "--project", p.Slug}
	}
	argJSON, _ := json.Marshal(args)
	return fmt.Sprintf(`{
  "mcpServers": {
    "%s": {
      "command": "%s",
      "args": %s,
      "description": "SkillsMCP project %s — globals + project skills. Start with list_skills."
    }
  }
}`, key, esc, argJSON, p.Slug)
}

// ClaudeConfigControl returns the management MCP stdio block.
func (a *App) ClaudeConfigControl() string {
	esc := strings.ReplaceAll(exePath(), `\`, `\\`)
	return fmt.Sprintf(`{
  "mcpServers": {
    "skillsmcp-control": {
      "command": "%s",
      "args": ["mcp", "--control"],
      "description": "SkillsMCP control — manage skills + projects. Start with app_help."
    }
  }
}`, esc)
}

// TestMCP performs the exact handshake an AI client does (main MCP).
func (a *App) TestMCP() string {
	base := a.mcpAddr
	if base == "" {
		base = "http://127.0.0.1:9423"
	}
	call := func(body string) (int, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "POST", base+"/mcp", strings.NewReader(body))
		if err != nil {
			return 0, err.Error()
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		s := string(b)
		if len(s) > 300 {
			s = s[:300] + "…"
		}
		return resp.StatusCode, s
	}
	var sb strings.Builder
	sb.WriteString("== stdio (`" + a.BinaryName() + " mcp`) ==\n")
	sb.WriteString(testMCPStdio())
	sb.WriteString("\n== http (127.0.0.1:9423/mcp) ==\n")
	st, b := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"skillsmcp-selftest","version":"0.1.0"}}}`)
	fmt.Fprintf(&sb, "initialize → %d %s\n", st, b)
	st, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	fmt.Fprintf(&sb, "notifications/initialized → %d (want 202, empty)\n", st)
	st, b = call(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	nTools := strings.Count(b, `"name":`)
	fmt.Fprintf(&sb, "tools/list → %d (%d tools)\n", st, nTools)
	st, b = call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_skills","arguments":{}}}`)
	fmt.Fprintf(&sb, "tools/call list_skills → %d %s", st, b)
	return sb.String()
}

// TestProjectMCP runs the stdio handshake against one project's MCP.
func (a *App) TestProjectMCP(slug string) string {
	p, ok := a.store.GetProjectBySlug(slug)
	if !ok {
		return "unknown project " + slug
	}
	return testMCPStdio("--project", p.Slug)
}

// TestWorkspaceMCP runs the stdio handshake against one workspace's MCP.
func (a *App) TestWorkspaceMCP(workspaceID string) string {
	w, ok := a.store.GetWorkspace(workspaceID)
	if !ok {
		return "unknown workspace"
	}
	if w.IsMain {
		return testMCPStdio()
	}
	return testMCPStdio("--workspace", w.Slug)
}

// TestProjectMCPIn runs the handshake against a workspace's project MCP.
func (a *App) TestProjectMCPIn(workspaceID string, slug string) string {
	w, ok := a.store.GetWorkspace(workspaceID)
	if !ok {
		return "unknown workspace"
	}
	p, ok := a.store.GetProjectBySlugIn(w.ID, slug)
	if !ok {
		return "unknown project " + slug
	}
	if w.IsMain {
		return testMCPStdio("--project", p.Slug)
	}
	return testMCPStdio("--workspace", w.Slug, "--project", p.Slug)
}

// TestControlMCP runs the stdio handshake against the management MCP.
func (a *App) TestControlMCP() string {
	return testMCPStdio("--control")
}

func testMCPStdio(extraArgs ...string) string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "stdio → cannot resolve own executable: " + fmt.Sprint(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdArgs := append([]string{"mcp"}, extraArgs...)
	cmd := exec.CommandContext(ctx, exe, cmdArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "stdio → stdin pipe: " + err.Error()
	}
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	if err := cmd.Start(); err != nil {
		return "stdio → start: " + err.Error()
	}
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"skillsmcp-selftest","version":"0.1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_skills","arguments":{}}}`,
	}
	for _, l := range lines {
		if _, err := io.WriteString(stdin, l+"\n"); err != nil {
			_ = cmd.Process.Kill()
			return "stdio → write: " + err.Error()
		}
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	out := strings.TrimSpace(outBuf.String())
	if out == "" {
		return "stdio → no output (subprocess died silently)"
	}
	var sb strings.Builder
	n := 0
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || !strings.HasPrefix(l, "{") {
			continue
		}
		n++
		if len(l) > 220 {
			l = l[:220] + "…"
		}
		fmt.Fprintf(&sb, "stdio msg %d → %s\n", n, l)
	}
	if n == 0 {
		if len(out) > 220 {
			out = out[:220] + "…"
		}
		return "stdio → no JSON-RPC replies. raw: " + out
	}
	return strings.TrimRight(sb.String(), "\n")
}
