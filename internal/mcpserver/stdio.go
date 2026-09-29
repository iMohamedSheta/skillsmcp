package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"skillsmcp/internal/applog"
	"skillsmcp/internal/store"
)

// Run starts the MCP stdio loop: the same binary runs as
// `SkillsMCP.exe mcp [--workspace <slug>] [--project <slug>] [--control]`,
// speaking JSON-RPC 2.0 NDJSON on stdin/stdout.
// No workspace = main (personal) workspace: `skillsmcp`, or
// skillsmcp-<project> with --project. With --workspace, the server is
// skillsmcp-<workspace> (or skillsmcp-<workspace>-<project>).
// With control = true, the server is skillsmcp-control and manages the app
// (all workspaces + write tools). Control cannot combine with a project
// (workspace is allowed: it scopes creations/lookups, default main).
//
// opencode global: { "mcp": { "skillsmcp": {
//   "type": "local", "command": ["<exe>", "mcp"], "enabled": true } } }
// opencode workspace: { "mcp": { "skillsmcp-team": {
//   "type": "local", "command": ["<exe>", "mcp", "--workspace", "team"], "enabled": true } } }
// opencode project: { "mcp": { "skillsmcp-myapp": {
//   "type": "local", "command": ["<exe>", "mcp", "--project", "myapp"], "enabled": true } } }
// opencode control: { "mcp": { "skillsmcp-control": {
//   "type": "local", "command": ["<exe>", "mcp", "--control"], "enabled": true } } }
//
// Protocol version: 2024-11-05. Nothing but JSON-RPC goes to stdout.
func Run(dbPath string, workspaceSlug string, projectSlug string, control bool) int {
	applog.Init(store.AppDir())
	st, err := store.Open(store.ResolveDBPath(dbPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillsmcp mcp: open db: %v\n", err)
		return 1
	}
	defer st.Close()
	ws := strings.ToLower(strings.TrimSpace(workspaceSlug))
	if ws == "" {
		ws = os.Getenv("SKILLSMCP_WORKSPACE")
	}
	wsID := ""
	if ws != "" {
		w, ok := st.GetWorkspaceBySlug(ws)
		if !ok {
			fmt.Fprintf(os.Stderr, "skillsmcp mcp: unknown workspace %q\n", ws)
			return 1
		}
		wsID = w.ID
	} else if w, ok := st.GetMainWorkspace(); ok {
		wsID = w.ID
	}
	sl := strings.ToLower(strings.TrimSpace(projectSlug))
	var srv *Server
	if control {
		srv = NewControl(st)
	} else if sl == "" {
		srv = NewIn(st, wsID)
	} else {
		if _, ok := st.GetProjectBySlugIn(wsID, sl); !ok {
			fmt.Fprintf(os.Stderr, "skillsmcp mcp: unknown project %q in this workspace\n", sl)
			return 1
		}
		srv = NewForProjectIn(st, wsID, sl)
	}
	name := srv.serverName()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 1024*1024*10)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var writeMu sync.Mutex
	write := func(id any, result any, rerr *rpcErr) {
		resp := rpcResp{JSONRPC: "2.0", ID: id, Result: result, Error: rerr}
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		writeMu.Lock()
		_, _ = out.Write(b)
		_ = out.Flush()
		writeMu.Unlock()
	}

	notifyToolsChanged := func() {
		n := map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"}
		b, _ := json.Marshal(n)
		b = append(b, '\n')
		writeMu.Lock()
		_, _ = out.Write(b)
		_ = out.Flush()
		writeMu.Unlock()
	}

	// Live updates: toolList() is already rebuilt from SQLite on every
	// tools/list call, but MCP clients cache the list until the server
	// tells them it changed. Poll the fingerprint and push the standard
	// notification so agents see new skills without restarting the app
	// or reconnecting the MCP. Control MCP has a static tool list — skip it.
	if !control {
		go func() {
			prev := toolsFingerprint(srv)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if cur := toolsFingerprint(srv); cur != prev {
					prev = cur
					notifyToolsChanged()
				}
			}
		}()
	}

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		if req.JSONRPC == "" {
			req.JSONRPC = "2.0"
		}
		isNotification := req.ID == nil

		switch req.Method {
		case "initialize":
			write(req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
				"serverInfo":      map[string]any{"name": name, "version": "0.1.0"},
			}, nil)
		case "notifications/initialized", "notifications/cancelled", "logging/setLevel":
			// no-op (notifications get no reply)
		case "ping":
			if !isNotification {
				write(req.ID, map[string]any{}, nil)
			}
		case "tools/list":
			write(req.ID, map[string]any{"tools": srv.toolList()}, nil)
		case "prompts/list":
			write(req.ID, map[string]any{"prompts": []any{}}, nil)
		case "resources/list":
			write(req.ID, map[string]any{"resources": []any{}}, nil)
		case "resources/templates/list":
			write(req.ID, map[string]any{"resourceTemplates": []any{}}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name == "" {
				var alt map[string]any
				_ = json.Unmarshal(req.Params, &alt)
				if n, ok := alt["name"].(string); ok {
					p.Name = n
					if a, ok := alt["arguments"].(map[string]any); ok {
						p.Arguments = a
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			out2, err := srv.callTool(ctx, p.Name, p.Arguments)
			cancel()
			if err != nil {
				write(req.ID, map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "error: " + err.Error()}},
					"isError": true,
				}, nil)
			} else {
				text, _ := json.MarshalIndent(out2, "", "  ")
				write(req.ID, map[string]any{
					"content": []any{map[string]any{"type": "text", "text": string(text)}},
				}, nil)
			}
		default:
			if !isNotification {
				write(req.ID, nil, &rpcErr{Code: -32601, Message: "method not found: " + req.Method})
			}
		}
	}
	return 0
}
