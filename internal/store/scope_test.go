package store

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "skills.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProjectScope(t *testing.T) {
	s := openTestDB(t)

	// seed: git-commit global exists
	globals := s.ListGlobalSkills(true)
	if len(globals) == 0 {
		t.Fatal("expected seeded git-commit global skill")
	}

	p, err := s.CreateProject(ProjectInput{Name: "Demo", Slug: "demo-app", Description: "d"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.Slug != "demo-app" {
		t.Fatalf("slug = %q", p.Slug)
	}

	ps, err := s.CreateSkill(SkillInput{
		Name: "demo-deploy", Description: "Deploy demo", Content: "# deploy",
		Scope: "project", ProjectID: p.ID, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create project skill: %v", err)
	}
	if ps.Scope != "project" || ps.ProjectID != p.ID {
		t.Fatalf("skill scope = %+v", ps)
	}

	// main MCP surface: globals only
	for _, g := range s.ListGlobalSkills(false) {
		if g.Scope == "project" {
			t.Fatalf("global list leaked project skill %q", g.Name)
		}
	}
	found := false
	for _, g := range s.ListGlobalSkills(false) {
		if g.Name == "git-commit" {
			found = true
		}
	}
	if !found {
		t.Fatal("git-commit missing from globals")
	}

	// project MCP surface: globals + project skill
	mcpSkills := s.ListProjectMCPSkills(p.ID, false)
	names := map[string]bool{}
	for _, sk := range mcpSkills {
		names[sk.Name] = true
	}
	if !names["git-commit"] || !names["demo-deploy"] {
		t.Fatalf("project MCP skills = %v", names)
	}

	// project-only listing
	only := s.ListProjectSkills(p.ID, false)
	if len(only) != 1 || only[0].Name != "demo-deploy" {
		t.Fatalf("project skills = %+v", only)
	}

	// delete project keeps skills as global
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	sk, ok := s.GetSkill("demo-deploy")
	if !ok {
		t.Fatal("project skill vanished on project delete")
	}
	if sk.Scope != "global" || sk.ProjectID != "" {
		t.Fatalf("after delete scope = %+v", sk)
	}
}

func TestMoveSkill(t *testing.T) {
	s := openTestDB(t)
	p, err := s.CreateProject(ProjectInput{Name: "Web", Slug: "web"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	sk, err := s.CreateSkill(SkillInput{Name: "x-deploy", Description: "d", Content: "c"})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	moved, err := s.MoveSkill(sk.ID, "project", p.ID)
	if err != nil {
		t.Fatalf("move to project: %v", err)
	}
	if moved.Scope != "project" || moved.ProjectID != p.ID {
		t.Fatalf("moved = %+v", moved)
	}
	// content untouched by the move
	if moved.Content != "c" || moved.Description != "d" {
		t.Fatalf("move altered content: %+v", moved)
	}
	if _, err := s.MoveSkill(sk.ID, "project", "no-such-id"); err == nil {
		t.Fatal("move to unknown project should fail")
	}
	back, err := s.MoveSkill(sk.ID, "global", "")
	if err != nil {
		t.Fatalf("move to global: %v", err)
	}
	if back.Scope != "global" || back.ProjectID != "" {
		t.Fatalf("back = %+v", back)
	}
	if _, err := s.MoveSkill("no-such-id", "global", ""); err == nil {
		t.Fatal("move unknown skill should fail")
	}
}
