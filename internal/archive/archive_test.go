package archive

import (
	"testing"

	"skillsmcp/internal/model"
)

func TestRoundTrip(t *testing.T) {
	skills := []model.Skill{
		{ID: "1", Name: "deploy-api", Description: "Deploy the API", Content: "# deploy\n\nsteps",
			Category: "deploy", Tags: "api", Scope: "global", Enabled: true, SortOrder: 2},
		{ID: "2", Name: "notes", Description: "Take notes", Content: "# notes",
			Category: "", Tags: "", Scope: "global", Enabled: false, SortOrder: 1},
	}
	m := FromSkills(skills, nil)
	if m.Scope != "global" || len(m.Skills) != 2 {
		t.Fatalf("manifest = %+v", m)
	}
	// Order preserved: notes (sort 1) first.
	if m.Skills[0].Name != "notes" || m.Skills[1].Name != "deploy-api" {
		t.Fatalf("order = %v", m.Skills)
	}
	zb, err := Build(m)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	back, err := Parse(zb)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(back.Skills) != 2 || back.Skills[0].Name != "notes" {
		t.Fatalf("round-trip = %+v", back.Skills)
	}
	if !back.Skills[1].Enabled || back.Skills[0].Enabled {
		t.Fatalf("enabled flags lost: %+v", back.Skills)
	}
	if back.Skills[1].Category != "deploy" || back.Skills[1].Tags != "api" {
		t.Fatalf("meta lost: %+v", back.Skills[1])
	}
	if back.Skills[1].Description != "Deploy the API" || back.Skills[1].Content != "# deploy\n\nsteps" {
		t.Fatalf("content lost: %+v", back.Skills[1])
	}
}

func TestProjectManifest(t *testing.T) {
	p := &model.Project{Name: "My App", Slug: "my-app", Description: "d", Color: "#6366f1"}
	m := FromSkills([]model.Skill{
		{Name: "x-deploy", Description: "d", Content: "c", Scope: "project", Enabled: true},
	}, p)
	if m.Scope != "project" || m.Project == nil || m.Project.Slug != "my-app" {
		t.Fatalf("manifest = %+v", m)
	}
	if m.FileName() == "" {
		t.Fatal("no filename")
	}
	zb, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(zb)
	if err != nil {
		t.Fatal(err)
	}
	if back.Project.Slug != "my-app" || len(back.Skills) != 1 {
		t.Fatalf("back = %+v", back)
	}
}

func TestBadManifests(t *testing.T) {
	if _, err := Unmarshal([]byte(`{}`)); err == nil {
		t.Fatal("empty manifest should fail")
	}
	if _, err := Unmarshal([]byte(`{"format":"nope","scope":"global"}`)); err == nil {
		t.Fatal("bad format should fail")
	}
	if _, err := Parse([]byte("not a zip")); err == nil {
		t.Fatal("non-zip should fail")
	}
}
