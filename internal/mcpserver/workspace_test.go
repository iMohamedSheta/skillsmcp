package mcpserver

import (
	"context"
	"testing"

	"skillsmcp/internal/store"
)

func TestWorkspaceMCPNames(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	team, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject(store.ProjectInput{Name: "API", WorkspaceID: team.ID}); err != nil {
		t.Fatal(err)
	}

	// Main workspace keeps the legacy names.
	if New(st).serverName() != "skillsmcp" {
		t.Fatalf("main name = %q", New(st).serverName())
	}
	if _, err := st.CreateProject(store.ProjectInput{Name: "Site"}); err != nil {
		t.Fatal(err)
	}
	if got := NewForProject(st, "site").serverName(); got != "skillsmcp-site" {
		t.Fatalf("main project name = %q", got)
	}
	// Workspace shapes.
	if got := NewIn(st, team.ID).serverName(); got != "skillsmcp-team" {
		t.Fatalf("ws name = %q", got)
	}
	if got := NewForProjectIn(st, team.ID, "api").serverName(); got != "skillsmcp-team-api" {
		t.Fatalf("ws project name = %q", got)
	}

	// Isolation: same skill name in both workspaces, each MCP serves its own.
	if _, err := st.CreateSkill(store.SkillInput{Name: "deploy", Description: "main d", Content: "c", Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	main, _ := st.GetMainWorkspace()
	if _, err := st.CreateSkill(store.SkillInput{Name: "deploy", Description: "team d", Content: "c", Scope: "global", WorkspaceID: team.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := New(st).callTool(ctx, "deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gm, _ := got.(map[string]any); gm["description"] != "main d" {
		t.Fatalf("main served wrong skill: %#v", got)
	}
	got, err = NewIn(st, team.ID).callTool(ctx, "deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gm, _ := got.(map[string]any); gm["description"] != "team d" {
		t.Fatalf("team served wrong skill: %#v", got)
	}
	_ = main

	// Project MCP in the team workspace serves team globals + api.
	if _, err := st.CreateSkill(store.SkillInput{Name: "team-only", Description: "t", Content: "c", Scope: "global", WorkspaceID: team.ID}); err != nil {
		t.Fatal(err)
	}
	pt := toolNames(NewForProjectIn(st, team.ID, "api").toolList())
	if !pt["team-only"] || !pt["deploy"] {
		t.Fatalf("team project tools = %v", pt)
	}
	if pt["list_projects"] {
		t.Fatal("project MCP should not offer list_projects")
	}
	// Main MCP does not leak team skills.
	if toolNames(New(st).toolList())["team-only"] {
		t.Fatal("main MCP leaked team skill")
	}
	// Team project MCP does not leak main globals.
	if _, err := st.CreateSkill(store.SkillInput{Name: "main-only", Description: "m", Content: "c", Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if toolNames(NewForProjectIn(st, team.ID, "api").toolList())["main-only"] {
		t.Fatal("team project MCP leaked main global")
	}
	if !toolNames(New(st).toolList())["main-only"] {
		t.Fatal("main MCP missing its own global")
	}
}
