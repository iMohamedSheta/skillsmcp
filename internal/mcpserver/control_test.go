package mcpserver

import (
	"context"
	"strings"
	"testing"

	"skillsmcp/internal/store"
)

func TestControlMCP(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	ctl := NewControl(st)

	if ctl.serverName() != "skillsmcp-control" {
		t.Fatalf("server name = %q", ctl.serverName())
	}

	names := toolNames(ctl.toolList())
	for _, want := range []string{
		"app_help", "list_skills", "get_skill", "list_projects", "list_project_skills",
		"create_skill", "update_skill", "delete_skill", "set_skill_enabled", "create_project",
		"update_project", "delete_project", "export_skills", "import_skills",
		"list_workspaces", "create_workspace", "update_workspace", "delete_workspace",
		"set_workspace_git", "push_workspace", "pull_workspace", "workspace_status",
		"workspace_conflicts", "reset_workspace", "clone_workspace",
	} {
		if !names[want] {
			t.Fatalf("control tools missing %q: %v", want, names)
		}
	}
	if names["git-commit"] {
		t.Fatal("control MCP should not expose skills as shortcut tools")
	}

	// help text exists
	out, err := ctl.callTool(ctx, "app_help", nil)
	if err != nil {
		t.Fatalf("app_help: %v", err)
	}
	m, _ := out.(map[string]any)
	help, _ := m["help"].(string)
	if !strings.Contains(help, "create_skill") {
		t.Fatalf("app_help too short: %q", help)
	}

	// create a project through the control MCP
	out, err = ctl.callTool(ctx, "create_project", map[string]any{"name": "Ctl App"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	pm, _ := out.(map[string]any)["project"].(map[string]any)
	if pm["slug"] != "ctl-app" {
		t.Fatalf("project slug = %#v", out)
	}

	// create global + project skills
	if _, err := ctl.callTool(ctx, "create_skill", map[string]any{
		"name": "ctl-notes", "description": "Take notes", "content": "# notes",
	}); err != nil {
		t.Fatalf("create global skill: %v", err)
	}
	if _, err := ctl.callTool(ctx, "create_skill", map[string]any{
		"name": "ctl-deploy", "description": "Deploy ctl", "content": "# deploy",
		"scope": "project", "project": "ctl-app",
	}); err != nil {
		t.Fatalf("create project skill: %v", err)
	}
	// project scope without slug is refused
	if _, err := ctl.callTool(ctx, "create_skill", map[string]any{
		"name": "ctl-bad", "description": "x", "content": "y", "scope": "project",
	}); err == nil {
		t.Fatal("project skill without slug should fail")
	}

	// control sees everything, including the project skill
	got, err := ctl.callTool(ctx, "get_skill", map[string]any{"name": "ctl-deploy"})
	if err != nil {
		t.Fatalf("control get_skill: %v", err)
	}
	if gm, _ := got.(map[string]any); gm["content"] != "# deploy" {
		t.Fatalf("content = %#v", got)
	}

	// main MCP: global visible, project hidden, no write tools
	main := New(st)
	mt := toolNames(main.toolList())
	if !mt["ctl-notes"] {
		t.Fatal("main MCP should serve the new global skill")
	}
	if mt["ctl-deploy"] || mt["create_skill"] || mt["app_help"] {
		t.Fatalf("main MCP leaked control scope: %v", mt)
	}
	if _, err := main.callTool(ctx, "get_skill", map[string]any{"name": "ctl-deploy"}); err == nil {
		t.Fatal("main get_skill served project skill, want refusal")
	}

	// disable the global skill: main refuses it, control still reads it
	if _, err := ctl.callTool(ctx, "set_skill_enabled", map[string]any{"name": "ctl-notes", "enabled": false}); err != nil {
		t.Fatalf("set_skill_enabled: %v", err)
	}
	if _, err := main.callTool(ctx, "ctl-notes", nil); err == nil {
		t.Fatal("main MCP served disabled skill, want refusal")
	}
	if _, err := ctl.callTool(ctx, "get_skill", map[string]any{"name": "ctl-notes"}); err != nil {
		t.Fatalf("control should still read disabled skills: %v", err)
	}

	// update + delete
	if _, err := ctl.callTool(ctx, "update_skill", map[string]any{"name": "ctl-notes", "description": "Take notes fast"}); err != nil {
		t.Fatalf("update_skill: %v", err)
	}
	sk, _ := st.GetSkill("ctl-notes")
	if sk.Description != "Take notes fast" || sk.Content != "# notes" {
		t.Fatalf("partial update broke other fields: %+v", sk)
	}
	if _, err := ctl.callTool(ctx, "delete_skill", map[string]any{"name": "ctl-notes"}); err != nil {
		t.Fatalf("delete_skill: %v", err)
	}
	if _, ok := st.GetSkill("ctl-notes"); ok {
		t.Fatal("skill still in store after delete")
	}

	// unknown tool + reserved name guard
	if _, err := ctl.callTool(ctx, "nope", nil); err == nil {
		t.Fatal("unknown control tool should fail")
	}
	if err := store.ValidateSkill("create_skill", "d", "c"); err == nil {
		t.Fatal("create_skill should be a reserved skill name")
	}
}

func TestExportImportSkills(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	ctl := NewControl(st)

	// Seed: one disabled global + one project with a skill.
	if _, err := ctl.callTool(ctx, "create_skill", map[string]any{
		"name": "arc-notes", "description": "Take notes", "content": "# notes",
		"category": "misc", "tags": "a,b",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	sk, _ := st.GetSkill("arc-notes")
	if _, err := st.SetEnabled(sk.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.callTool(ctx, "create_project", map[string]any{"name": "Arc App"}); err != nil {
		t.Fatalf("create_project: %v", err)
	}
	if _, err := ctl.callTool(ctx, "create_skill", map[string]any{
		"name": "arc-deploy", "description": "Deploy", "content": "# deploy",
		"scope": "project", "project": "arc-app",
	}); err != nil {
		t.Fatalf("create project skill: %v", err)
	}

	// Export globals → wipe → re-import restores everything losslessly.
	out, err := ctl.callTool(ctx, "export_skills", map[string]any{"scope": "global"})
	if err != nil {
		t.Fatalf("export globals: %v", err)
	}
	arch, _ := out.(map[string]any)["archive"].(string)
	if arch == "" {
		t.Fatalf("empty archive: %#v", out)
	}
	if _, err := ctl.callTool(ctx, "delete_skill", map[string]any{"name": "arc-notes"}); err != nil {
		t.Fatal(err)
	}
	back, err := ctl.callTool(ctx, "import_skills", map[string]any{"archive": arch})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if bm, _ := back.(map[string]any); bm["imported"] != 1 {
		t.Fatalf("imported = %#v", back)
	}
	got, ok := st.GetSkill("arc-notes")
	if !ok || got.Category != "misc" || got.Tags != "a,b" || got.Enabled {
		t.Fatalf("not restored losslessly: %+v", got)
	}
	// Re-import is a no-op for existing names (skipped, never overwritten).
	back2, err := ctl.callTool(ctx, "import_skills", map[string]any{"archive": arch})
	if err != nil {
		t.Fatal(err)
	}
	if bm, _ := back2.(map[string]any); bm["imported"] != 0 {
		t.Fatalf("re-import should skip: %#v", back2)
	}

	// Export project → delete project → re-import recreates project + skill.
	pout, err := ctl.callTool(ctx, "export_skills", map[string]any{"scope": "project", "project": "arc-app"})
	if err != nil {
		t.Fatalf("export project: %v", err)
	}
	parch, _ := pout.(map[string]any)["archive"].(string)
	if _, err := ctl.callTool(ctx, "delete_project", map[string]any{"project": "arc-app"}); err != nil {
		t.Fatalf("delete_project: %v", err)
	}
	if _, ok := st.GetProjectBySlug("arc-app"); ok {
		t.Fatal("project still there")
	}
	pback, err := ctl.callTool(ctx, "import_skills", map[string]any{"archive": parch})
	if err != nil {
		t.Fatalf("import project: %v", err)
	}
	if bm, _ := pback.(map[string]any); bm["imported"] != 1 || bm["project"] != "arc-app" {
		t.Fatalf("project import = %#v", pback)
	}
	if _, ok := st.GetSkill("arc-deploy"); !ok {
		t.Fatal("project skill not restored")
	}
	// Project MCP serves it again.
	proj := NewForProject(st, "arc-app")
	if !toolNames(proj.toolList())["arc-deploy"] {
		t.Fatal("project MCP missing restored skill")
	}

	// Bad archives are refused.
	if _, err := ctl.callTool(ctx, "import_skills", map[string]any{"archive": "nope"}); err == nil {
		t.Fatal("garbage archive should fail")
	}
}
