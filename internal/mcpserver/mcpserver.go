// Package mcpserver exposes the skill library over MCP (stdio + HTTP).
//
// Scopes:
//   - MAIN MCP (no project): serves GLOBAL skills only, plus
//     list_projects + list_project_skills so the AI can discover projects.
//     Every enabled global skill is also its own tool.
//   - PROJECT MCP (`mcp --project <slug>`): serves globals + that
//     project's skills. Server name is skillsmcp-<slug>.
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
// ProjectID == "" means the main/global MCP.
type Server struct {
	st        *store.Store
	projectID string
	mu        sync.Mutex
	http      *http.Server
	listener  net.Listener
	addr      string
	running   bool
}

func New(st *store.Store) *Server { return &Server{st: st} }

// NewForProject builds a project-scoped server (globals + that project).
// Unknown slug falls back to main/global (callers should validate first).
func NewForProject(st *store.Store, projectSlug string) *Server {
	sl := strings.ToLower(strings.TrimSpace(projectSlug))
	if sl == "" {
		return &Server{st: st}
	}
	if p, ok := st.GetProjectBySlug(sl); ok {
		return &Server{st: st, projectID: p.ID}
	}
	return &Server{st: st}
}

func (s *Server) ProjectID() string {
	return s.projectID
}

func (s *Server) project() (model.Project, bool) {
	if s.projectID == "" {
		return model.Project{}, false
	}
	return s.st.GetProject(s.projectID)
}

func (s *Server) serverName() string {
	if p, ok := s.project(); ok {
		return "skillsmcp-" + p.Slug
	}
	return "skillsmcp"
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

// scopedSkills: main = globals only; project = globals + that project.
func (s *Server) scopedSkills() []model.Skill {
	if s.projectID == "" {
		return s.st.ListGlobalSkills(false)
	}
	return s.st.ListProjectMCPSkills(s.projectID, false)
}

// toolList is rebuilt on EVERY call so adding/enabling a skill
// immediately shows up as a new MCP tool (no restart needed).
func (s *Server) toolList() []toolDef {
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

func (s *Server) listDesc() string {
	if p, ok := s.project(); ok {
		return fmt.Sprintf("Index of skills in project %q (globals + project skills). Start every session here, then call the matching skill tool or get_skill.", p.Slug)
	}
	return "Index of GLOBAL skills (name + description + category). Start every session here, then call the matching skill tool or get_skill. Project skills live behind their own MCP — see list_projects."
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	_ = ctx
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
			for _, m := range s.st.ListSummariesForProject("") {
				rows = append(rows, row{Name: m.Name, Description: m.Description, Category: m.Category, Tags: m.Tags, Scope: "global", UpdatedAt: m.UpdatedAt})
			}
		} else {
			for _, m := range s.st.ListSummariesForProject(s.projectID) {
				sc := m.Scope
				if sc == "" {
					sc = "global"
				}
				rows = append(rows, row{Name: m.Name, Description: m.Description, Category: m.Category, Tags: m.Tags, Scope: sc, Project: m.ProjectSlug, UpdatedAt: m.UpdatedAt})
			}
		}
		hint := "Call the matching skill tool (e.g. git-commit) or get_skill {name} for the full instruction."
		if s.projectID == "" {
			hint += " Need project skills? Call list_projects, then switch to that project's MCP (`mcp --project <slug>`)."
		}
		return map[string]any{"skills": rows, "hint": hint}, nil
	case "get_skill":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		sk, ok := s.st.GetSkill(n)
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
		projs := s.st.ListProjects()
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
		return map[string]any{
			"projects": rows,
			"hint":     "Each project has its own MCP: `SkillsMCP mcp --project <slug>` serves globals + that project's skills.",
		}, nil
	case "list_project_skills":
		if s.projectID != "" {
			return nil, fmt.Errorf("list_project_skills lives on the main MCP only")
		}
		slug := str("project")
		if slug == "" {
			return nil, fmt.Errorf("project required (e.g. {\"project\": \"my-app\"}) — call list_projects first")
		}
		p, ok := s.st.GetProjectBySlug(slug)
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
		return map[string]any{
			"project": p.Slug, "skills": rows,
			"hint": fmt.Sprintf("Switch to this project's MCP for full content: `SkillsMCP mcp --project %s`, then list_skills.", p.Slug),
		}, nil
	default:
		sk, ok := s.st.GetSkill(name)
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
		"content": sk.Content,
	}
}

func sScope(sk model.Skill) string {
	if sk.Scope == "" {
		return "global"
	}
	return sk.Scope
}

// visible enforces scope: main sees globals only; project sees globals + own.
func (s *Server) visible(sk model.Skill) bool {
	if sScope(sk) == "global" || sk.ProjectID == "" {
		return true
	}
	return s.projectID != "" && sk.ProjectID == s.projectID
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
		return fmt.Sprintf("it is a project skill — use its project MCP: `SkillsMCP mcp --project %s`", proj)
	}
	return fmt.Sprintf("it belongs to another project — switch to `SkillsMCP mcp --project %s`", proj)
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
			"capabilities":    map[string]any{"tools": map[string]any{}},
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
