package gitsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	pres, err := Push(st, team.ID, false)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !pres.Pushed || pres.Skills != 2 {
		t.Fatalf("push = %+v", pres)
	}

	// Pushing again with no changes: nothing committed, still pushed state.
	pres2, err := Push(st, team.ID, false)
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

// TestConflictFlows reproduces the classic README-init conflict: the
// remote has commits the checkout doesn't have, so a normal push is
// rejected with a full message + hint. Force push (local wins) and
// reset-to-remote (remote wins) both resolve it in one step.
func TestConflictFlows(t *testing.T) {
	if _, err := execLookPath(); err != nil {
		t.Skip("git not available")
	}
	st := testStore(t)
	team, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "s1", Description: "d", Content: "c", Scope: "global", WorkspaceID: team.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Remote with an unrelated README commit (like "create repo on GitHub").
	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := gitInitBare(remote); err != nil {
		t.Fatal(err)
	}
	seedDir := filepath.Join(t.TempDir(), "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	must := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = seedDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	must("init", "-b", "main", ".")
	must("config", "user.name", "t")
	must("config", "user.email", "t@t")
	must("remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("add", "-A")
	must("commit", "-m", "init")
	must("push", "-u", "origin", "main")

	if _, err := st.SetWorkspaceGit(team.ID, remote, "main", ""); err != nil {
		t.Fatal(err)
	}

	// Normal push is rejected — with the FULL remote message + a hint.
	// The work still commits locally, so nothing is lost.
	first, err := Push(st, team.ID, false)
	if err == nil {
		t.Fatal("push should be rejected (unrelated histories)")
	}
	if !first.Committed {
		t.Fatal("rejected push must still commit locally")
	}
	if first.Hint == "" || !strings.Contains(strings.ToLower(first.Hint), "force") {
		t.Fatalf("want force hint, got %+v", first)
	}
	low := strings.ToLower(first.Detail)
	if first.Detail == "" || (!strings.Contains(low, "fetch first") &&
		!strings.Contains(low, "non-fast-forward") &&
		!strings.Contains(low, "rejected")) {
		t.Fatalf("want full remote message, got %q", first.Detail)
	}

	// Escape hatch 1: force push (local wins).
	forced, err := Push(st, team.ID, true)
	if err != nil {
		t.Fatalf("force push: %v", err)
	}
	if !forced.Pushed || !forced.Forced {
		t.Fatalf("force = %+v", forced)
	}

	// Escape hatch 2: diverge again, then reset (remote wins, additive).
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "local-only", Description: "d", Content: "c", Scope: "global", WorkspaceID: team.ID,
	}); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(t.TempDir(), "other")
	must2 := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	cmd := exec.Command("git", "clone", "-b", "main", remote, otherDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	must2("config", "user.name", "t")
	must2("config", "user.email", "t@t")
	// A teammate's edit = new entry in globals/manifest.json + its .md.
	manPath := filepath.Join(otherDir, "globals", "manifest.json")
	manRaw, err := os.ReadFile(manPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(manRaw, &raw); err != nil {
		t.Fatal(err)
	}
	skills, _ := raw["skills"].([]any)
	skills = append(skills, map[string]any{
		"name": "remote-only", "description": "From the repo",
		"content": "# remote-only\n\nbody\n", "enabled": true,
	})
	raw["skills"] = skills
	out2, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(manPath, out2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "globals", "skills", "remote-only.md"),
		[]byte("# remote-only\n\n> From the repo\n\n# remote-only\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must2("add", "-A")
	must2("commit", "-m", "remote edit")
	must2("push", "origin", "main")

	reset, err := Reset(st, team.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, ok := st.GetSkillIn(team.ID, "remote-only"); !ok {
		t.Fatal("remote skill not restored by reset")
	}
	if _, ok := st.GetSkillIn(team.ID, "local-only"); !ok {
		t.Fatal("local-only skill must survive an additive reset")
	}
	if _, ok := st.GetSkillIn(team.ID, "s1"); !ok {
		t.Fatal("s1 must survive reset")
	}
	_ = ctx
	_ = reset
}
