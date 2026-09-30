package store

import (
	"path/filepath"
	"testing"
)

func openWSStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMainWorkspaceSeeded(t *testing.T) {
	s := openWSStore(t)
	w, ok := s.GetMainWorkspace()
	if !ok || w.Slug != "main" || !w.IsMain {
		t.Fatalf("main workspace = %+v %v", w, ok)
	}
	if w.Name != "Personal" {
		t.Fatalf("default workspace name = %q, want Personal", w.Name)
	}
	if len(s.ListWorkspaces()) != 1 {
		t.Fatal("want exactly the main workspace")
	}
	// Seeded skill lives in main.
	sk, ok := s.GetSkill("git-commit")
	if !ok {
		t.Fatal("seed skill missing")
	}
	if sk.WorkspaceID != w.ID {
		t.Fatalf("seed skill workspace = %q", sk.WorkspaceID)
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	s := openWSStore(t)
	main, _ := s.GetMainWorkspace()
	team, err := s.CreateWorkspace(WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if team.Slug != "team" || team.IsMain {
		t.Fatalf("workspace = %+v", team)
	}
	// Same skill name in both workspaces is fine (unique per workspace).
	for _, ws := range []string{main.ID, team.ID} {
		if _, err := s.CreateSkill(SkillInput{
			Name: "deploy", Description: "d " + ws, Content: "c",
			Scope: "global", WorkspaceID: ws, Enabled: true,
		}); err != nil {
			t.Fatalf("create in %s: %v", ws, err)
		}
	}
	m1, _ := s.GetSkillIn(main.ID, "deploy")
	m2, _ := s.GetSkillIn(team.ID, "deploy")
	if m1.Description == m2.Description {
		t.Fatal("workspaces leak into each other")
	}
	if len(s.ListGlobalSkillsIn(team.ID, true)) != 1 {
		t.Fatal("team globals wrong")
	}
	// Same project slug in both workspaces.
	p1, err := s.CreateProject(ProjectInput{Name: "API", WorkspaceID: main.ID})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProject(ProjectInput{Name: "API", WorkspaceID: team.ID})
	if err != nil {
		t.Fatalf("slug reuse across workspaces: %v", err)
	}
	if p2.WorkspaceID != team.ID {
		t.Fatal("project landed in the wrong workspace")
	}
	if _, err := s.CreateProject(ProjectInput{Name: "API", WorkspaceID: team.ID}); err == nil {
		t.Fatal("duplicate slug in same workspace should fail")
	}
	// Lookup scoping.
	if _, ok := s.GetProjectBySlugIn(team.ID, "api"); !ok {
		t.Fatal("team project lookup")
	}
	if got, ok := s.GetProjectBySlugIn(main.ID, "api"); !ok || got.ID != p1.ID {
		t.Fatal("main project lookup hit the wrong workspace")
	}
	// Delete cascades; main is protected.
	if err := s.DeleteWorkspace(main.ID); err == nil {
		t.Fatal("main workspace must be protected")
	}
	if err := s.DeleteWorkspace(team.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.GetSkillIn(team.ID, "deploy"); ok {
		t.Fatal("team skills should be gone")
	}
	if _, ok := s.GetProjectBySlugIn(team.ID, "api"); ok {
		t.Fatal("team projects should be gone")
	}
	if _, ok := s.GetSkill("git-commit"); !ok {
		t.Fatal("main skills must survive")
	}
}

func TestLegacyMigration(t *testing.T) {
	// Simulate a pre-workspace database: old shape, one skill + project.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "skills.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Force legacy shape: drop new objects, recreate old ones with data.
	_, _ = s.db.Exec(`DROP TABLE skills`)
	_, _ = s.db.Exec(`DROP TABLE projects`)
	_, _ = s.db.Exec(`DELETE FROM workspaces`)
	_, _ = s.db.Exec(`CREATE TABLE skills(
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT 'global', project_id TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')`)
	_, _ = s.db.Exec(`CREATE TABLE projects(
		id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '',
		slug TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '',
		color TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '')`)
	_, _ = s.db.Exec(`INSERT INTO skills(id,name,description,content) VALUES('s1','legacy','d','c')`)
	_, _ = s.db.Exec(`INSERT INTO projects(id,name,slug) VALUES('p1','Old','old')`)
	_ = s.Close()

	// Reopen → migrate must adopt everything into main.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	main, ok := s2.GetMainWorkspace()
	if !ok {
		t.Fatal("no main workspace after migration")
	}
	sk, ok := s2.GetSkillIn(main.ID, "legacy")
	if !ok || sk.WorkspaceID != main.ID {
		t.Fatalf("legacy skill not adopted: %+v %v", sk, ok)
	}
	if _, ok := s2.GetProjectBySlugIn(main.ID, "old"); !ok {
		t.Fatal("legacy project not adopted")
	}
	// Per-workspace uniqueness now holds: reuse the name in a new workspace.
	team, err := s2.CreateWorkspace(WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.CreateSkill(SkillInput{Name: "legacy", Description: "d", Content: "c", Scope: "global", WorkspaceID: team.ID}); err != nil {
		t.Fatalf("per-workspace name reuse: %v", err)
	}
}

func TestWorkspaceTokenRules(t *testing.T) {
	s := openWSStore(t)
	team, err := s.CreateWorkspace(WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if team.HasToken {
		t.Fatal("fresh workspace should have no token")
	}
	// Link with token → stored + flagged, never readable via getters.
	linked, err := s.SetWorkspaceGit(team.ID, "https://github.com/o/r.git", "main", "sekret")
	if err != nil {
		t.Fatal(err)
	}
	if !linked.HasToken {
		t.Fatal("token flag missing after link")
	}
	if got, _ := s.GetWorkspace(team.ID); !got.HasToken {
		t.Fatal("flag missing on read")
	}
	if tok := s.WorkspaceGitToken(team.ID); tok != "sekret" {
		t.Fatalf("token not stored: %q", tok)
	}
	// Empty token keeps the stored one.
	kept, err := s.SetWorkspaceGit(team.ID, "https://github.com/o/r.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if !kept.HasToken || s.WorkspaceGitToken(team.ID) != "sekret" {
		t.Fatal("empty token should keep the stored one")
	}
	// Changing remote without a token drops the old one (no leaking).
	moved, err := s.SetWorkspaceGit(team.ID, "git@github.com:o/other.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.HasToken || s.WorkspaceGitToken(team.ID) != "" {
		t.Fatal("remote change must drop the old token")
	}
	// Unlinking drops everything.
	_, _ = s.SetWorkspaceGit(team.ID, "https://github.com/o/r.git", "main", "sekret")
	unlinked, err := s.SetWorkspaceGit(team.ID, "", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if unlinked.GitRemote != "" || unlinked.HasToken {
		t.Fatal("unlink must clear remote + token")
	}
}
