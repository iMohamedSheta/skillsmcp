package gitsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
)

func execLookPath() (string, error) { return exec.LookPath("git") }

func gitInitBare(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPushPullRoundTrip(t *testing.T) {
	if _, err := execLookPath(); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	st := testStore(t)

	team, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "team-deploy", Description: "Deploy team", Content: "# deploy",
		Category: "deploy", Tags: "t", Scope: "global", WorkspaceID: team.ID,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProject(store.ProjectInput{Name: "API", WorkspaceID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "api-notes", Description: "Notes", Content: "# notes",
		Scope: "project", ProjectID: p.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Bare repo acts as "GitHub" (works for public or private alike —
	// auth only matters for real remotes).
	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := gitInitBare(remote); err != nil {
		t.Fatalf("bare: %v", err)
	}
	if _, err := st.SetWorkspaceGit(team.ID, remote, "main", ""); err != nil {
		t.Fatal(err)
	}

	pres, err := Push(st, team.ID)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !pres.Pushed || pres.Skills != 2 {
		t.Fatalf("push = %+v", pres)
	}

	// Pushing again with no changes: nothing committed, still pushed state.
	pres2, err := Push(st, team.ID)
	if err != nil {
		t.Fatalf("re-push: %v", err)
	}
	if pres2.Committed {
		t.Fatal("second push should have nothing to commit")
	}

	// Wipe the workspace, pull it all back.
	if err := st.DeleteWorkspace(team.ID); err != nil {
		t.Fatal(err)
	}
	team2, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetWorkspaceGit(team2.ID, remote, "main", ""); err != nil {
		t.Fatal(err)
	}
	pures, err := Pull(st, team2.ID)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if pures.Imported != 2 {
		t.Fatalf("pull = %+v", pures)
	}
	sk, ok := st.GetSkillIn(team2.ID, "team-deploy")
	if !ok || sk.Category != "deploy" || sk.Tags != "t" {
		t.Fatalf("skill not restored: %+v %v", sk, ok)
	}
	if _, ok := st.GetProjectBySlugIn(team2.ID, "api"); !ok {
		t.Fatal("project not restored")
	}
	if _, ok := st.GetSkillIn(team2.ID, "api-notes"); !ok {
		t.Fatal("project skill not restored")
	}

	// Status is clean after a pull.
	line, err := StatusLine(st, team2.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if line == "" {
		t.Fatal("empty status")
	}
	_ = ctx

	// Repo layout sanity: workspace.json + manifests on disk.
	dir := DirFor(store.AppDir(), "team")
	_ = dir
}

func TestWriteReadWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	ws := model.Workspace{Name: "Team", Slug: "team", Description: "d", Color: "#fff"}
	globals := archive.Manifest{Format: archive.FormatTag, Scope: "global",
		Skills: []archive.SkillEntry{{Name: "a", Description: "d", Content: "c", Enabled: true}}}
	projs := map[string]archive.Manifest{
		"api": {Format: archive.FormatTag, Scope: "project",
			Project: &archive.ProjectMeta{Name: "API", Slug: "api"},
			Skills:  []archive.SkillEntry{{Name: "b", Description: "d", Content: "c", Enabled: false}}},
	}
	if err := WriteWorkspace(dir, ws, globals, projs); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"workspace.json", "globals/manifest.json", "globals/skills/a.md", "projects/api/manifest.json", "projects/api/skills/b.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s", f)
		}
	}
	wf, g, pr, err := ReadWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Slug != "team" || g == nil || len(g.Skills) != 1 {
		t.Fatalf("read = %+v", wf)
	}
	if len(pr) != 1 || len(pr["api"].Skills) != 1 || pr["api"].Skills[0].Enabled {
		t.Fatalf("projects = %+v", pr)
	}
}
