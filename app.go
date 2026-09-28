package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"skillsmcp/internal/applog"
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
	sb.WriteString(testMCPStdio("", ""))
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

func testMCPStdio(flag, value string) string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "stdio → cannot resolve own executable: " + fmt.Sprint(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if flag == "" {
		cmd = exec.CommandContext(ctx, exe, "mcp")
	} else {
		cmd = exec.CommandContext(ctx, exe, "mcp", flag, value)
	}
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
