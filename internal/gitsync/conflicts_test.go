package gitsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/store"
)

func TestGetConflictsModifiedAndResolve(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Isolate checkouts per test.
	t.Setenv("SKILLSMCP_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Conf Team"})
	if err != nil {
		t.Fatal(err)
	}
	sk, err := st.CreateSkill(store.SkillInput{
		Name: "s1", Description: "d", Content: "# v1",
		Scope: "global", WorkspaceID: ws.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := gitInitBare(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetWorkspaceGit(ws.ID, remote, "main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(st, ws.ID, false); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Local edit.
	if _, err := st.UpdateSkill(sk.ID, store.SkillInput{
		Name: "s1", Description: "d-local", Content: "# v2 local",
		Scope: "global", WorkspaceID: ws.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Remote edit from a second clone.
	otherDir := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-b", "main", remote, otherDir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	must := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	must("config", "user.name", "t")
	must("config", "user.email", "t@t")
	manPath := filepath.Join(otherDir, "globals", "manifest.json")
	raw, err := os.ReadFile(manPath)
	if err != nil {
		t.Fatal(err)
	}
	// Minimal edit: replace description + content strings.
	edited := strings.Replace(string(raw), `"description": "d"`, `"description": "d-remote"`, 1)
	edited = strings.Replace(edited, "# v1", "# v2 remote", 1)
	if edited == string(raw) {
		t.Fatalf("manifest edit did not match: %s", raw)
	}
	if err := os.WriteFile(manPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	mdPath := filepath.Join(otherDir, "globals", "skills", "s1.md")
	if mdRaw, err := os.ReadFile(mdPath); err == nil {
		mdEdited := strings.Replace(string(mdRaw), "# v1", "# v2 remote", 1)
		mdEdited = strings.Replace(mdEdited, "> d\n", "> d-remote\n", 1)
		_ = os.WriteFile(mdPath, []byte(mdEdited), 0o644)
	}
	must("add", "-A")
	must("commit", "-m", "remote edit s1")
	must("push", "origin", "main")

	conf, err := GetConflicts(st, ws.ID)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if len(conf.Skills) != 1 {
		t.Fatalf("skills = %+v", conf.Skills)
	}
	sc := conf.Skills[0]
	if sc.Kind != "modified" {
		t.Fatalf("kind = %q want modified (%+v)", sc.Kind, sc)
	}
	if len(sc.ChangedFields) == 0 {
		t.Fatalf("no changed fields: %+v", sc)
	}
	hasDesc, hasContent := false, false
	for _, f := range sc.ChangedFields {
		if f == "description" {
			hasDesc = true
		}
		if f == "content" {
			hasContent = true
		}
	}
	if !hasDesc || !hasContent {
		t.Fatalf("changedFields = %v", sc.ChangedFields)
	}
	// Files list covers every part.
	foundManifest, foundSkill := false, false
	for _, f := range conf.Files {
		if f.Path == "globals/manifest.json" && f.Status == "modified" {
			foundManifest = true
		}
		if f.Path == "globals/skills/s1.md" && f.Status == "modified" {
			foundSkill = true
		}
	}
	if !foundManifest || !foundSkill {
		t.Fatalf("files = %+v", conf.Files)
	}

	// Resolve: take remote.
	res, err := Resolve(st, ws.ID,
		[]SkillResolution{{Name: "s1", Scope: "global", Action: "take-remote"}},
		nil, "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Updated != 1 {
		t.Fatalf("resolve = %+v", res)
	}
	got, ok := st.GetSkillIn(ws.ID, "s1")
	if !ok || got.Description != "d-remote" || got.Content != "# v2 remote" {
		t.Fatalf("after take-remote: %+v %v", got, ok)
	}

	// Resolve merged via our editor payload.
	if _, err := st.UpdateSkill(got.ID, store.SkillInput{
		Name: "s1", Description: "d-local2", Content: "# v3 local",
		Scope: "global", WorkspaceID: ws.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	mergedContent := "# merged\n\nbest of both"
	res2, err := Resolve(st, ws.ID,
		[]SkillResolution{{Name: "s1", Scope: "global", Action: "merged",
			Merged: &archive.SkillEntry{Name: "s1", Description: "d-merged", Content: mergedContent, Enabled: true}}},
		nil, "", nil)
	if err != nil {
		t.Fatalf("merged resolve: %v", err)
	}
	if res2.Updated != 1 {
		t.Fatalf("merged resolve = %+v", res2)
	}
	got2, _ := st.GetSkillIn(ws.ID, "s1")
	if got2.Content != mergedContent || got2.Description != "d-merged" {
		t.Fatalf("merged not applied: %+v", got2)
	}
}

func TestGetConflictsAddedSides(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("SKILLSMCP_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws, err := st.CreateWorkspace(store.WorkspaceInput{Name: "Add Team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "local-only", Description: "d", Content: "c",
		Scope: "global", WorkspaceID: ws.ID,
	}); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := gitInitBare(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetWorkspaceGit(ws.ID, remote, "main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(st, ws.ID, false); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Remote-only skill via second clone.
	otherDir := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-b", "main", remote, otherDir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	must := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	must("config", "user.name", "t")
	must("config", "user.email", "t@t")
	manPath := filepath.Join(otherDir, "globals", "manifest.json")
	raw, err := os.ReadFile(manPath)
	if err != nil {
		t.Fatal(err)
	}
	// Append a new skill entry by string surgery (keeps format valid).
	s := string(raw)
	idx := strings.LastIndex(s, "]")
	if idx < 0 {
		t.Fatalf("bad manifest: %s", s)
	}
	entry := `, {"name": "remote-only", "description": "From repo", "content": "# remote", "enabled": true}]`
	s = s[:idx] + entry + s[idx+1:]
	if err := os.WriteFile(manPath, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(otherDir, "globals", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "globals", "skills", "remote-only.md"),
		[]byte("# remote-only\n\n> From repo\n\n# remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("add", "-A")
	must("commit", "-m", "remote-only")
	must("push", "origin", "main")

	// Local-only skill added after push.
	if _, err := st.CreateSkill(store.SkillInput{
		Name: "local-new", Description: "d", Content: "c",
		Scope: "global", WorkspaceID: ws.ID,
	}); err != nil {
		t.Fatal(err)
	}

	conf, err := GetConflicts(st, ws.ID)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	kinds := map[string]string{}
	for _, sc := range conf.Skills {
		kinds[sc.Name] = sc.Kind
	}
	if kinds["local-only"] != "unchanged" && kinds["local-only"] != "added-local" {
		// local-only was pushed, remote has it too (unchanged) unless remote
		// clone dropped it — either is fine except modified.
		if kinds["local-only"] == "modified" {
			t.Fatalf("local-only kind = %q", kinds["local-only"])
		}
	}
	if kinds["remote-only"] != "added-remote" {
		t.Fatalf("remote-only kind = %q (%+v)", kinds["remote-only"], conf.Skills)
	}
	if kinds["local-new"] != "added-local" {
		t.Fatalf("local-new kind = %q", kinds["local-new"])
	}

	// Union resolve: keep local-new, import remote-only.
	res, err := Resolve(st, ws.ID,
		[]SkillResolution{
			{Name: "remote-only", Scope: "global", Action: "take-remote"},
			{Name: "local-new", Scope: "global", Action: "keep-local"},
		}, nil, "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := st.GetSkillIn(ws.ID, "remote-only"); !ok {
		t.Fatalf("remote-only not imported: %+v", res)
	}
	if _, ok := st.GetSkillIn(ws.ID, "local-new"); !ok {
		t.Fatal("local-new lost")
	}
}
