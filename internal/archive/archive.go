// Package archive implements lossless skill backup files.
//
// An archive is a .zip with:
//   manifest.json — full-fidelity JSON (every field needed to restore
//                   skills exactly: name, description, content, category,
//                   tags, enabled, order + the project meta for project
//                   exports). This is the source of truth on import.
//   skills/<name>.md — one human-readable file per skill, in the same
//                   `# name / > description / content` shape as the
//                   single-skill export, so the zip stays useful even
//                   without the manifest.
//
// Round-trip: export globals or one project → .zip → import the same
// .zip later (even into a fresh database) → every skill comes back
// with name, description, content, category, tags, enabled state and
// relative order intact. Project archives also recreate the project
// (name, slug, description, color) when its slug doesn't exist yet.
// Duplicate skill names are skipped, never overwritten — same rule as
// the .md bulk import.
package archive

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"skillsmcp/internal/model"
)

// FormatTag identifies manifests this package can read.
const FormatTag = "skillsmcp-archive/v1"

// Manifest is the interchange format. The control MCP export/import
// tools speak exactly this JSON (no zip needed for agents); the UI
// wraps it in a .zip together with readable .md files.
type Manifest struct {
	Format     string       `json:"format"`
	ExportedAt string       `json:"exportedAt"`
	Scope      string       `json:"scope"` // "global" or "project"
	Project    *ProjectMeta `json:"project,omitempty"`
	Skills     []SkillEntry `json:"skills"`
}

// ProjectMeta is the project backup (project-scope archives only).
type ProjectMeta struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// SkillEntry is one skill with every restorable field.
type SkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Category    string `json:"category,omitempty"`
	Tags        string `json:"tags,omitempty"`
	Enabled     bool   `json:"enabled"`
	SortOrder   int    `json:"sortOrder,omitempty"`
}

// FromSkills builds a manifest from store models, preserving order.
func FromSkills(skills []model.Skill, project *model.Project) Manifest {
	scope := "global"
	var meta *ProjectMeta
	if project != nil {
		scope = "project"
		meta = &ProjectMeta{
			Name: project.Name, Slug: project.Slug,
			Description: project.Description, Color: project.Color,
		}
	}
	// Deterministic order: manual position first, name as tiebreak —
	// the same order the UI shows, so re-import restores the layout.
	ordered := append([]model.Skill(nil), skills...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SortOrder != ordered[j].SortOrder {
			return ordered[i].SortOrder < ordered[j].SortOrder
		}
		return ordered[i].Name < ordered[j].Name
	})
	m := Manifest{
		Format: FormatTag, ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Scope: scope, Project: meta, Skills: []SkillEntry{},
	}
	for _, s := range ordered {
		m.Skills = append(m.Skills, SkillEntry{
			Name: s.Name, Description: s.Description, Content: s.Content,
			Category: s.Category, Tags: s.Tags,
			Enabled: s.Enabled, SortOrder: s.SortOrder,
		})
	}
	return m
}

// Marshal returns the canonical manifest JSON.
func (m Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Unmarshal parses + validates manifest JSON.
func Unmarshal(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("not a skills archive manifest: %v", err)
	}
	if m.Format != FormatTag {
		return Manifest{}, fmt.Errorf("unsupported archive format %q (want %q)", m.Format, FormatTag)
	}
	if m.Scope != "global" && m.Scope != "project" {
		return Manifest{}, fmt.Errorf("archive scope must be global|project (got %q)", m.Scope)
	}
	for i, s := range m.Skills {
		if strings.TrimSpace(s.Name) == "" {
			return Manifest{}, fmt.Errorf("skill #%d has no name", i+1)
		}
		if strings.TrimSpace(s.Content) == "" {
			return Manifest{}, fmt.Errorf("skill %q has no content", s.Name)
		}
		if strings.TrimSpace(s.Description) == "" {
			return Manifest{}, fmt.Errorf("skill %q has no description", s.Name)
		}
	}
	return m, nil
}

// FileName suggests `skillsmcp-<scope>-<date>.zip`.
func (m Manifest) FileName() string {
	slug := "global"
	if m.Scope == "project" && m.Project != nil && m.Project.Slug != "" {
		slug = m.Project.Slug
	}
	return fmt.Sprintf("skillsmcp-%s-%s.zip", slug, time.Now().Format("20060102"))
}

// MarkdownFile renders one skill in the single-export shape
// (`# name`, `> description`, body) so plain-.md importers read it.
func MarkdownFile(e SkillEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n> %s\n\n%s", e.Name, e.Description, e.Content)
	if !strings.HasSuffix(e.Content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// Build zips a manifest: manifest.json + skills/<name>.md per skill.
func Build(m Manifest) ([]byte, error) {
	raw, err := m.Marshal()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	mf, err := w.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write(raw); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range m.Skills {
		name := s.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		f, err := w.Create("skills/" + name + ".md")
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(MarkdownFile(s))); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Parse reads a zip built by Build: manifest.json is authoritative.
// Zips without a manifest (e.g. hand-packed .md collections) fall back
// to parsing each .md via heading/quote heuristics.
func Parse(zipBytes []byte) (Manifest, error) {
	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return Manifest{}, fmt.Errorf("not a zip archive: %v", err)
	}
	for _, f := range r.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				return Manifest{}, err
			}
			var buf bytes.Buffer
			_, cpErr := buf.ReadFrom(rc)
			_ = rc.Close()
			if cpErr != nil {
				return Manifest{}, cpErr
			}
			return Unmarshal(buf.Bytes())
		}
	}
	// Fallback: no manifest — parse markdown files.
	m := Manifest{Format: FormatTag, ExportedAt: time.Now().UTC().Format(time.RFC3339), Scope: "global", Skills: []SkillEntry{}}
	for _, f := range r.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".md") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		_ = rc.Close()
		text := buf.String()
		if strings.TrimSpace(text) == "" {
			continue
		}
		name, desc := parseMarkdown(f.Name, text)
		if name == "" || desc == "" {
			continue
		}
		m.Skills = append(m.Skills, SkillEntry{Name: name, Description: desc, Content: text, Enabled: true})
	}
	if len(m.Skills) == 0 {
		return Manifest{}, fmt.Errorf("archive has no manifest.json and no importable .md files")
	}
	return m, nil
}

// parseMarkdown mirrors the UI bulk-import heuristic: name from the
// first `# heading` (fallback: filename), description from the first
// `> quote` line.
func parseMarkdown(fileName, text string) (name, desc string) {
	for _, ln := range strings.Split(text, "\n") {
		if name == "" && strings.HasPrefix(ln, "# ") {
			name = slugify(strings.TrimSpace(strings.TrimPrefix(ln, "# ")))
			continue
		}
		if desc == "" && strings.HasPrefix(strings.TrimSpace(ln), ">") {
			desc = strings.TrimSpace(strings.TrimSpace(ln)[1:])
		}
	}
	if name == "" {
		base := fileName
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if i := strings.LastIndex(base, "."); i > 0 {
			base = base[:i]
		}
		name = slugify(base)
	}
	return name, desc
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
