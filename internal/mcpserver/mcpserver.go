// Package mcpserver exposes the skill library over MCP (stdio + HTTP).
//
// Scopes:
//   - MAIN MCP (no project): serves one workspace's GLOBAL skills, plus
//     list_projects + list_project_skills so the AI can discover projects.
//     Every enabled global skill is also its own tool.
//   - PROJECT MCP (`mcp --project <slug>`): serves that workspace's
//     globals + that project's skills.
//   - WORKSPACE MCP (`mcp --workspace <slug>`): the same two shapes for
//     another workspace. No --workspace means the main (personal) one,
//     whose server names stay exactly as before (`skillsmcp`,
//     `skillsmcp-<project>`) so existing client configs keep working.
//     Other workspaces are `skillsmcp-<workspace>` and
//     `skillsmcp-<workspace>-<project>`.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
)

// Server serves MCP over loopback HTTP plus plain REST the UI uses.
// WorkspaceID selects the workspace (resolved to the main workspace
// when empty). ProjectID == "" means the workspace's main/global MCP.
// Control == true means the management MCP (skillsmcp-control): read
// everything + write tools across all workspaces.
type Server struct {
	st          *store.Store
	workspaceID string
	projectID   string
	control     bool
	mu          sync.Mutex
	http        *http.Server
	listener    net.Listener
	addr        string
	running     bool
}

// New builds the main workspace's main MCP (`skillsmcp`).
func New(st *store.Store) *Server { return NewIn(st, "") }

// NewIn builds one workspace's main MCP.
func NewIn(st *store.Store, workspaceID string) *Server {
	if strings.TrimSpace(workspaceID) == "" {
		if w, ok := st.GetMainWorkspace(); ok {
			workspaceID = w.ID
		}
	}
	return &Server{st: st, workspaceID: workspaceID}
}

// NewControl builds the management server (all scopes + write tools).
// It ignores projects: control sees everything.
func NewControl(st *store.Store) *Server {
	return &Server{st: st, control: true}
}

func (s *Server) IsControl() bool {
	return s.control
}

// NewForProject builds a main-workspace project-scoped server.
// Unknown slug falls back to main/global (callers should validate first).
func NewForProject(st *store.Store, projectSlug string) *Server {
	return NewForProjectIn(st, "", projectSlug)
}

// NewForProjectIn builds a workspace's project-scoped server (that
// workspace's globals + that project's skills).
func NewForProjectIn(st *store.Store, workspaceID, projectSlug string) *Server {
	srv := NewIn(st, workspaceID)
	sl := strings.ToLower(strings.TrimSpace(projectSlug))
	if sl == "" {
		return srv
	}
	if p, ok := st.GetProjectBySlugIn(srv.workspaceID, sl); ok {
		srv.projectID = p.ID
	}
	return srv
}

func (s *Server) ProjectID() string {
	return s.projectID
}

// WorkspaceID reports the workspace this server reads.
func (s *Server) WorkspaceID() string {
	return s.workspaceID
}

func (s *Server) project() (model.Project, bool) {
	if s.projectID == "" {
		return model.Project{}, false
	}
	return s.st.GetProject(s.projectID)
}

func (s *Server) workspace() (model.Workspace, bool) {
	if s.workspaceID == "" {
		return s.st.GetMainWorkspace()
	}
	return s.st.GetWorkspace(s.workspaceID)
}

// isMainWorkspace reports whether this server reads the personal workspace.
func (s *Server) isMainWorkspace() bool {
	w, ok := s.workspace()
	return ok && w.IsMain
}

func (s *Server) serverName() string {
	if s.control {
		return "skillsmcp-control"
	}
	w, ok := s.workspace()
	if !ok || w.IsMain {
		if p, pok := s.project(); pok {
			return "skillsmcp-" + p.Slug
		}
		return "skillsmcp"
	}
	if p, pok := s.project(); pok {
		return "skillsmcp-" + w.Slug + "-" + p.Slug
	}
	return "skillsmcp-" + w.Slug
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcResp struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcErr     `json:"error,omitempty"`
}
type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

func emptyObj() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
}

// ToolNames returns the current tool names (for UI previews).
func (s *Server) ToolNames() []string {
	tools := s.toolList()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// scopedSkills: main = workspace globals; project = workspace globals + project.
func (s *Server) scopedSkills() []model.Skill {
	if s.projectID == "" {
		return s.st.ListGlobalSkillsIn(s.workspaceID, false)
	}
	return s.st.ListProjectMCPSkills(s.projectID, false)
}

// toolList is rebuilt on EVERY call so adding/enabling a skill
// immediately shows up as a new MCP tool (no restart needed).
// Clients learn about it via tools/list re-query + the
// notifications/tools/list_changed push (see stdio.go watcher).
func (s *Server) toolList() []toolDef {
	if s.control {
		out := []toolDef{
			{
				Name:        "list_skills",
				Description: s.listDesc(),
				InputSchema: emptyObj(),
			},
		{
			Name:        "get_skill",
			Description: "Fetch the full markdown instruction for one skill by name (any workspace + scope, even disabled). Use after list_skills when you know which skill you need.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string", "description": "Skill name, e.g. git-commit"},
					"workspace": map[string]any{"type": "string", "description": "Workspace slug, default main."},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "list_projects",
			Description: "List projects that have their own MCP (slug + description + skill counts + workspace).",
			InputSchema: emptyObj(),
		},
		{
			Name:        "list_project_skills",
			Description: "List skill names in one project (index only, no content). Use get_skill for the full instruction.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":   map[string]any{"type": "string", "description": "Project slug, e.g. my-app"},
					"workspace": map[string]any{"type": "string", "description": "Workspace slug, default main. Required when the slug exists in several workspaces."},
				},
				"required": []string{"project"},
			},
		},
	}
		return append(out, controlToolDefs()...)
	}
	skills := s.scopedSkills()
	out := []toolDef{
		{
			Name:        "list_skills",
			Description: s.listDesc(),
			InputSchema: emptyObj(),
		},
		{
			Name:        "get_skill",
			Description: "Fetch the full markdown instruction for one skill by name. Use after list_skills when you know which skill you need.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string", "description": "Skill name, e.g. git-commit"}},
				"required":   []string{"name"},
			},
		},
	}
	if s.projectID == "" {
		out = append(out,
			toolDef{
				Name:        "list_projects",
				Description: "List projects that have their own MCP (slug + description). Each project MCP serves globals + that project's skills via `mcp --project <slug>`.",
				InputSchema: emptyObj(),
			},
			toolDef{
				Name:        "list_project_skills",
				Description: "List skill names in one project (index only, no content). Use get_skill for the full instruction.",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"project": map[string]any{"type": "string", "description": "Project slug, e.g. my-app"}},
					"required":   []string{"project"},
				},
			},
		)
	}
	for _, sk := range skills {
		out = append(out, toolDef{
			Name:        sk.Name,
			Description: sk.Description,
			InputSchema: emptyObj(),
		})
	}
	return out
}

// toolsFingerprint is a cheap change detector for the stdio watcher.
// Any add/rename/enable/description edit changes the joined names,
// so the watcher can push notifications/tools/list_changed.
func toolsFingerprint(s *Server) string {
	tools := s.toolList()
	var b strings.Builder
	for _, t := range tools {
		b.WriteString(t.Name)
		b.WriteString("\x00")
		b.WriteString(t.Description)
		b.WriteString("\x00;")
	}
	return b.String()
}

func (s *Server) listDesc() string {
	if s.control {
		return "Management index: EVERY skill in every workspace and scope (global + all projects), including disabled ones. Start every management session here, then get_skill / create_skill / update_skill as needed. Call app_help first for the full playbook."
	}
	if p, ok := s.project(); ok {
		if w, wok := s.workspace(); wok && !w.IsMain {
			return fmt.Sprintf("Index of skills in workspace %q project %q (workspace globals + project skills). Start every session here, then call the matching skill tool or get_skill.", w.Slug, p.Slug)
		}
		return fmt.Sprintf("Index of skills in project %q (globals + project skills). Start every session here, then call the matching skill tool or get_skill.", p.Slug)
	}
	if w, wok := s.workspace(); wok && !w.IsMain {
		return fmt.Sprintf("Index of workspace %q GLOBAL skills (name + description + category). Start every session here, then call the matching skill tool or get_skill.", w.Slug)
	}
	return "Index of GLOBAL skills (name + description + category). Start every session here, then call the matching skill tool or get_skill. Project skills live behind their own MCP — see list_projects."
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	_ = ctx
	if s.control {
		return s.callControlTool(name, args)
	}
	str := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	switch name {
	case "list_skills":
		type row struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category,omitempty"`
			Tags        string `json:"tags,omitempty"`
			Scope       string `json:"scope,omitempty"`
			Project     string `json:"project,omitempty"`
			UpdatedAt   string `json:"updatedAt,omitempty"`
		}
		rows := []row{}
		if s.projectID == "" {
			for _, m := range s.st.ListSummariesForProjectIn(s.workspaceID, "") {
				rows = append(rows, row{Name: m.Name, Description: m.Description, Category: m.Category, Tags: m.Tags, Scope: "global", UpdatedAt: m.UpdatedAt})
			}
		} else {
			for _, m := range s.st.ListSummariesForProjectIn(s.workspaceID, s.projectID) {
				sc := m.Scope
				if sc == "" {
					sc = "global"
				}
				rows = append(rows, row{Name: m.Name, Description: m.Description, Category: m.Category, Tags: m.Tags, Scope: sc, Project: m.ProjectSlug, UpdatedAt: m.UpdatedAt})
			}
		}
		hint := "Call the matching skill tool (e.g. git-commit) or get_skill {name} for the full instruction."
		if s.projectID == "" {
			hint += " Need project skills? Call list_projects, then switch to that project's MCP."
			if !s.isMainWorkspace() {
				if w, ok := s.workspace(); ok {
					hint += fmt.Sprintf(" (this workspace: `mcp --workspace %s --project <slug>`)", w.Slug)
				}
			} else {
				hint += " (`mcp --project <slug>`)"

			}
		}
		return map[string]any{"skills": rows, "hint": hint}, nil
	case "get_skill":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		sk, ok := s.st.GetSkillIn(s.workspaceID, n)
		if !ok {
			return nil, fmt.Errorf("unknown skill %q — call list_skills first", n)
		}
		if !sk.Enabled {
			return nil, fmt.Errorf("skill %q is disabled — enable it in SkillsMCP first", n)
		}
		if !s.visible(sk) {
			return nil, fmt.Errorf("skill %q is not in this MCP scope — %s", n, s.scopeHint(sk))
		}
		return skillPayload(sk), nil
	case "list_projects":
		if s.projectID != "" {
			return nil, fmt.Errorf("list_projects lives on the main MCP only — this is the %q project MCP", s.serverName())
		}
		projs := s.st.ListProjectsIn(s.workspaceID)
		type prow struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description,omitempty"`
			Skills      int    `json:"skills"`
		}
		rows := []prow{}
		for _, p := range projs {
			rows = append(rows, prow{Name: p.Name, Slug: p.Slug, Description: p.Description, Skills: s.st.CountProjectSkills(p.ID)})
		}
		hint := "Each project has its own MCP serving workspace globals + that project's skills."
		if w, ok := s.workspace(); ok && !w.IsMain {
			hint += fmt.Sprintf(" (`SkillsMCP mcp --workspace %s --project <slug>`.", w.Slug)
		} else {
			hint += " (`SkillsMCP mcp --project <slug>`)."
		}
		return map[string]any{
			"projects": rows,
			"hint":     hint,
		}, nil
	case "list_project_skills":
		if s.projectID != "" {
			return nil, fmt.Errorf("list_project_skills lives on the main MCP only")
		}
		slug := str("project")
		if slug == "" {
			return nil, fmt.Errorf("project required (e.g. {\"project\": \"my-app\"}) — call list_projects first")
		}
		p, ok := s.st.GetProjectBySlugIn(s.workspaceID, slug)
		if !ok {
			return nil, fmt.Errorf("unknown project %q — call list_projects first", slug)
		}
		type row struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		rows := []row{}
		for _, sk := range s.st.ListProjectSkills(p.ID, false) {
			rows = append(rows, row{Name: sk.Name, Description: sk.Description})
		}
		projCmd := fmt.Sprintf("SkillsMCP mcp --project %s", p.Slug)
		if w, ok := s.workspace(); ok && !w.IsMain {
			projCmd = fmt.Sprintf("SkillsMCP mcp --workspace %s --project %s", w.Slug, p.Slug)
		}
		return map[string]any{
			"project": p.Slug, "skills": rows,
			"hint": fmt.Sprintf("Switch to this project's MCP for full content: `%s`, then list_skills.", projCmd),
		}, nil
	default:
		sk, ok := s.st.GetSkillIn(s.workspaceID, name)
		if !ok {
			return nil, fmt.Errorf("unknown tool %q — call list_skills to see available skills", name)
		}
		if !sk.Enabled {
			return nil, fmt.Errorf("skill %q is disabled", name)
		}
		if !s.visible(sk) {
			return nil, fmt.Errorf("skill %q is not in this MCP scope — %s", name, s.scopeHint(sk))
		}
		return skillPayload(sk), nil
	}
}

func skillPayload(sk model.Skill) map[string]any {
	return map[string]any{
		"name": sk.Name, "description": sk.Description,
		"category": sk.Category, "tags": sk.Tags,
		"scope": sScope(sk), "project": sk.ProjectSlug,
		"workspace": sk.WorkspaceSlug,
		"content": sk.Content,
	}
}

func sScope(sk model.Skill) string {
	if sk.Scope == "" {
		return "global"
	}
	return sk.Scope
}

// visible enforces scope: same workspace required; main sees globals
// only; project sees globals + own project.
func (s *Server) visible(sk model.Skill) bool {
	if sk.WorkspaceID != "" && s.workspaceID != "" && sk.WorkspaceID != s.workspaceID {
		return false
	}
	if sScope(sk) == "global" || sk.ProjectID == "" {
		return true
	}
	return s.projectID != "" && sk.ProjectID == s.projectID
}

func (s *Server) mcpCommand(projSlug string) string {
	if w, ok := s.workspace(); ok && !w.IsMain {
		if projSlug != "" {
			return fmt.Sprintf("SkillsMCP mcp --workspace %s --project %s", w.Slug, projSlug)
		}
		return fmt.Sprintf("SkillsMCP mcp --workspace %s", w.Slug)
	}
	if projSlug != "" {
		return fmt.Sprintf("SkillsMCP mcp --project %s", projSlug)
	}
	return "SkillsMCP mcp"
}

func (s *Server) scopeHint(sk model.Skill) string {
	if sScope(sk) == "global" {
		return "unreachable — this should not happen"
	}
	proj := sk.ProjectSlug
	if proj == "" {
		if p, ok := s.st.GetProject(sk.ProjectID); ok {
			proj = p.Slug
		}
	}
	if proj == "" {
		proj = "<slug>"
	}
	if s.projectID == "" {
		return fmt.Sprintf("it is a project skill — use its project MCP: `%s`", s.mcpCommand(proj))
	}
	return fmt.Sprintf("it belongs to another project — switch to `%s`", s.mcpCommand(proj))
}

// Start binds 127.0.0.1:preferredPort (or ephemeral fallback) and serves MCP + REST.
func (s *Server) Start(preferredPort int) (string, error) {
	s.mu.Lock()
	if s.running {
		a := s.addr
		s.mu.Unlock()
		return a, nil
	}
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/mcp/tools", s.handleTools)
	mux.HandleFunc("/api/skills", s.handleSkills)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"` + s.serverName() + `"}`))
	})

	addr := "127.0.0.1:9423"
	if preferredPort != 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", preferredPort)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil && preferredPort != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
	}
	srv := &http.Server{Handler: cors(mux), ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.http = srv
	s.listener = ln
	s.addr = "http://" + ln.Addr().String()
	s.running = true
	s.mu.Unlock()
	go srv.Serve(ln)
	return s.addr, nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.http.Shutdown(ctx)
	}
	s.running = false
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == "OPTIONS" {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"tools": s.toolList(), "server": s.serverName()})
}
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if s.projectID == "" {
		writeJSON(w, map[string]any{"skills": s.st.ListSummariesForProject(""), "server": s.serverName()})
		return
	}
	writeJSON(w, map[string]any{"skills": s.st.ListSummariesForProject(s.projectID), "server": s.serverName()})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	name := s.serverName()
	if r.Method == "GET" {
		writeJSON(w, map[string]any{
			"name": name, "version": "0.1.0",
			"description": fmt.Sprintf("%s — skill library. Start with list_skills.", name),
			"tools":       s.toolList(),
		})
		return
	}
	var req rpcReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	switch req.Method {
	case "initialize":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": name, "version": "0.1.0"},
		}})
	case "ping":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": s.toolList()}})
	case "prompts/list":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"prompts": []any{}}})
	case "resources/list":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"resources": []any{}}})
	case "resources/templates/list":
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"resourceTemplates": []any{}}})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			// Handlers may return context (detail/hint) alongside the
			// error — forward it so clients see the full message + fix.
			if m, ok := out.(map[string]any); ok && len(m) > 0 {
				text, _ := json.MarshalIndent(m, "", "  ")
				writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
					"content": []any{map[string]any{"type": "text", "text": string(text)}},
					"isError": true,
				}})
				return
			}
			writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			}})
			return
		}
		text, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []any{map[string]any{"type": "text", "text": string(text)}},
		}})
	default:
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{Code: -32601, Message: "unknown method " + req.Method}})
	}
}
