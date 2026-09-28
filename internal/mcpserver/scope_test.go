package mcpserver

import (
	"context"
	"path/filepath"
	"testing"

	"skillsmcp/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func toolNames(tools []toolDef) map[string]bool {
	m := map[string]bool{}
	for _, tl := range tools {
		m[tl.Name] = true
	}
	return m
}

func TestMainVsProjectMCP(t *testing.T) {
	st := openTestStore(t)
	p, err := st.CreateProject(store.ProjectInput{Name: "Demo", Slug: "demo-app"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "demo-deploy", Description: "Deploy demo", Content: "# deploy",
		Scope: "project", ProjectID: p.ID, Enabled: true,
	}); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	ctx := context.Background()
	main := New(st)

	mt := toolNames(main.toolList())
	if !mt["list_skills"] || !mt["get_skill"] || !mt["list_projects"] || !mt["list_project_skills"] || !mt["git-commit"] {
		t.Fatalf("main tools = %v", mt)
	}
	if mt["demo-deploy"] {
		t.Fatal("main MCP leaked project skill as tool")
	}
	// main MCP refuses project skill content
	if _, err := main.callTool(ctx, "demo-deploy", nil); err == nil {
		t.Fatal("main MCP served project skill, want refusal")
	}
	if _, err := main.callTool(ctx, "get_skill", map[string]any{"name": "demo-deploy"}); err == nil {
		t.Fatal("main get_skill served project skill, want refusal")
	}
	// discovery works
	out, err := main.callTool(ctx, "list_projects", nil)
	if err != nil {
		t.Fatalf("list_projects: %v", err)
	}
	m, _ := out.(map[string]any)
	if m == nil || m["projects"] == nil {
		t.Fatalf("list_projects payload = %#v", out)
	}

	proj := NewForProject(st, "demo-app")
	pt := toolNames(proj.toolList())
	if !pt["git-commit"] || !pt["demo-deploy"] {
		t.Fatalf("project tools = %v", pt)
	}
	if pt["list_projects"] {
		t.Fatal("project MCP should not offer list_projects")
	}
	got, err := proj.callTool(ctx, "demo-deploy", nil)
	if err != nil {
		t.Fatalf("project MCP demo-deploy: %v", err)
	}
	gm, _ := got.(map[string]any)
	if gm["content"] != "# deploy" {
		t.Fatalf("project skill content = %#v", got)
	}
	if proj.serverName() != "skillsmcp-demo-app" {
		t.Fatalf("server name = %q", proj.serverName())
	}
}
