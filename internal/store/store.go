// Package store persists skills + projects in SQLite (pure Go, no CGO).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"skillsmcp/internal/model"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Store persists skills + projects + settings in SQLite (WAL).
// DB file: ~/.skillsmcp/skills.db (0600). Override with SKILLSMCP_HOME.
type Store struct {
	mu     sync.Mutex
	dir    string
	dbPath string
	db     *sql.DB
}

func AppDir() string {
	if v := os.Getenv("SKILLSMCP_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skillsmcp")
}

func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func ResolveDBPath(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("SKILLSMCP_DB_PATH"); v != "" {
		return v
	}
	return filepath.Join(AppDir(), "skills.db")
}

func New() (*Store, error) {
	return Open(ResolveDBPath(""))
}

func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, dbPath: path}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s.db = db
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, err
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	if err := s.seedDefaults(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.dbPath }

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

const baseTables = `
CREATE TABLE IF NOT EXISTS projects(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	slug TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS skills(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	tags TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL DEFAULT 'global',
	project_id TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS settings(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

var indexStmts = []string{
	`CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects(slug)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_name ON skills(name)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_scope ON skills(scope)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_project ON skills(project_id)`,
}

// migrate runs in three phases so old (pre-scope) databases upgrade cleanly:
// 1. base tables (new full shape; no-op when they already exist),
// 2. additive ALTERs for columns old tables lack (duplicate errors ignored),
// 3. indexes LAST — they reference the new columns and would abort the
//    whole migration with "no such column" if built before the ALTERs.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(baseTables); err != nil {
		return err
	}
	for _, col := range []string{
		`ALTER TABLE skills ADD COLUMN scope TEXT NOT NULL DEFAULT 'global'`,
		`ALTER TABLE skills ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = s.db.Exec(col)
	}
	for _, idx := range indexStmts {
		_, _ = s.db.Exec(idx)
	}
	return nil
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]{0,63}$`)
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

var reserved = map[string]bool{
	"list_skills": true, "get_skill": true, "list_projects": true,
	"list_project_skills": true, "fleet_overview": true,
	"initialize": true, "ping": true,
}

// NormalizeName lowercases, trims, converts spaces to dashes.
func NormalizeName(raw string) string {
	n := strings.ToLower(strings.TrimSpace(raw))
	n = strings.ReplaceAll(n, " ", "-")
	n = strings.ReplaceAll(n, "_", "-")
	var b strings.Builder
	for _, r := range n {
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

// NormalizeSlug is the project equivalent (no underscores).
func NormalizeSlug(raw string) string {
	n := strings.ToLower(strings.TrimSpace(raw))
	n = strings.ReplaceAll(n, " ", "-")
	n = strings.ReplaceAll(n, "_", "-")
	var b strings.Builder
	for _, r := range n {
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

// ValidateSkill checks name/description/content before save.
func ValidateSkill(name, description, content string) error {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return fmt.Errorf("name is required (e.g. git-commit)")
	}
	if reserved[n] {
		return fmt.Errorf("name %q is reserved — pick another", n)
	}
	if !nameRe.MatchString(n) {
		return fmt.Errorf("name must match [a-z0-9-_], start alnum, max 64 (got %q). Hint: use like git-commit", name)
	}
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("description is required — the AI reads it to decide when to call this skill")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content is required — the markdown instruction returned to the AI")
	}
	return nil
}

func ValidateProject(name, slug string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("project name is required")
	}
	sl := strings.ToLower(strings.TrimSpace(slug))
	if sl == "" {
		return fmt.Errorf("project slug is required (e.g. my-app)")
	}
	if reserved[sl] {
		return fmt.Errorf("slug %q is reserved", sl)
	}
	if !slugRe.MatchString(sl) {
		return fmt.Errorf("slug must match [a-z0-9-], start alnum, max 64 (got %q)", slug)
	}
	return nil
}

// ---- skills CRUD (scope-aware) ----

const skillCols = `id,name,description,content,category,tags,scope,project_id,enabled,created_at,updated_at`

func scanSkill(row interface {
	Scan(dest ...any) error
}) (model.Skill, error) {
	var x model.Skill
	var en int
	err := row.Scan(&x.ID, &x.Name, &x.Description, &x.Content, &x.Category, &x.Tags,
		&x.Scope, &x.ProjectID, &en, &x.CreatedAt, &x.UpdatedAt)
	x.Enabled = en == 1
	if x.Scope == "" {
		x.Scope = "global"
	}
	return x, err
}

// attachProjects fills ProjectSlug/ProjectName for a batch (single query).
func (s *Store) attachProjects(out []model.Skill) []model.Skill {
	if len(out) == 0 {
		return out
	}
	rows, err := s.db.Query(`SELECT id,slug,name FROM projects`)
	if err != nil {
		return out
	}
	defer rows.Close()
	byID := map[string]model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name); err == nil {
			byID[p.ID] = p
		}
	}
	for i := range out {
		if out[i].ProjectID != "" {
			if p, ok := byID[out[i].ProjectID]; ok {
				out[i].ProjectSlug = p.Slug
				out[i].ProjectName = p.Name
			}
		}
	}
	return out
}

func (s *Store) querySkills(q string, args ...any) []model.Skill {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Skill
	for rows.Next() {
		x, err := scanSkill(rows)
		if err != nil {
			continue
		}
		out = append(out, x)
	}
	return s.attachProjects(out)
}

// ListSkills returns ALL skills (any scope). Kept for the UI + compat.
func (s *Store) ListSkills(includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT ` + skillCols + ` FROM skills ORDER BY name`)
	}
	return s.querySkills(`SELECT ` + skillCols + ` FROM skills WHERE enabled=1 ORDER BY name`)
}

// ListGlobalSkills returns enabled (or all) global skills for the MAIN MCP.
func (s *Store) ListGlobalSkills(includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT ` + skillCols + ` FROM skills WHERE scope='global' OR scope='' ORDER BY name`)
	}
	return s.querySkills(`SELECT ` + skillCols + ` FROM skills WHERE (scope='global' OR scope='') AND enabled=1 ORDER BY name`)
}

// ListProjectSkills returns a single project's skills.
func (s *Store) ListProjectSkills(projectID string, includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE project_id=? ORDER BY name`, projectID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE project_id=? AND enabled=1 ORDER BY name`, projectID)
}

// ProjectMCP Skills = globals + that project's skills (enabled only unless asked).
func (s *Store) ListProjectMCPSkills(projectID string, includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE (scope='global' OR scope='') OR project_id=? ORDER BY scope DESC, name`, projectID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE ((scope='global' OR scope='') OR project_id=?) AND enabled=1 ORDER BY scope DESC, name`, projectID)
}

// ListSummaries is the lightweight MAIN-MCP index: global skills only.
func (s *Store) ListSummaries() []model.SkillSummary {
	return s.ListSummariesForProject("")
}

// ListSummariesForProject: "" = globals only; projectID = globals + that project.
func (s *Store) ListSummariesForProject(projectID string) []model.SkillSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.db.Query(`SELECT name,description,category,tags,scope,project_id,enabled,updated_at FROM skills WHERE (scope='global' OR scope='') AND enabled=1 ORDER BY name`)
	} else {
		rows, err = s.db.Query(`SELECT name,description,category,tags,scope,project_id,enabled,updated_at FROM skills WHERE ((scope='global' OR scope='') OR project_id=?) AND enabled=1 ORDER BY name`, projectID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	// project slug lookup for the summary rows
	prows, _ := s.db.Query(`SELECT id,slug FROM projects`)
	slugByID := map[string]string{}
	if prows != nil {
		for prows.Next() {
			var id, slug string
			if err := prows.Scan(&id, &slug); err == nil {
				slugByID[id] = slug
			}
		}
		prows.Close()
	}
	var out []model.SkillSummary
	for rows.Next() {
		var m model.SkillSummary
		var en int
		var pid string
		if err := rows.Scan(&m.Name, &m.Description, &m.Category, &m.Tags, &m.Scope, &pid, &en, &m.UpdatedAt); err == nil {
			m.Enabled = en == 1
			if m.Scope == "" {
				m.Scope = "global"
			}
			m.ProjectSlug = slugByID[pid]
			out = append(out, m)
		}
	}
	return out
}

func (s *Store) GetSkill(name string) (model.Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := strings.ToLower(strings.TrimSpace(name))
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE name=?`, n)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, false
	}
	// attach project slug/name
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, true
}

func (s *Store) GetSkillByID(id string) (model.Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, false
	}
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, true
}

type SkillInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	Scope       string `json:"scope"`
	ProjectID   string `json:"projectId"`
	Enabled     bool   `json:"enabled"`
}

func normalizeScope(in string, projectID string) (string, string) {
	sc := strings.ToLower(strings.TrimSpace(in))
	if sc != "project" {
		return "global", ""
	}
	return "project", strings.TrimSpace(projectID)
}

func (s *Store) resolveScopeLocked(scope, projectID string) (string, string, error) {
	sc, pid := normalizeScope(scope, projectID)
	if sc == "project" {
		if pid == "" {
			return "", "", fmt.Errorf("project is required for project-scoped skills")
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, pid).Scan(&n); err != nil || n == 0 {
			return "", "", fmt.Errorf("unknown project — pick one from the Projects tab")
		}
	}
	return sc, pid, nil
}

func (s *Store) CreateSkill(in SkillInput) (model.Skill, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if err := ValidateSkill(name, in.Description, in.Content); err != nil {
		return model.Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, pid, err := s.resolveScopeLocked(in.Scope, in.ProjectID)
	if err != nil {
		return model.Skill{}, err
	}
	now := Now()
	x := model.Skill{
		ID:          uuid.NewString(),
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Content:     in.Content,
		Category:    strings.TrimSpace(in.Category),
		Tags:        strings.TrimSpace(in.Tags),
		Scope:       scope,
		ProjectID:   pid,
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.db.Exec(`INSERT INTO skills(id,name,description,content,category,tags,scope,project_id,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.Name, x.Description, x.Content, x.Category, x.Tags, x.Scope, x.ProjectID, 1, x.CreatedAt, x.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Skill{}, fmt.Errorf("skill %q already exists — pick another name or edit it", name)
		}
		return model.Skill{}, err
	}
	if pid != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, pid).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, nil
}

func (s *Store) UpdateSkill(id string, in SkillInput) (model.Skill, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if err := ValidateSkill(name, in.Description, in.Content); err != nil {
		return model.Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var curName string
	err := s.db.QueryRow(`SELECT name FROM skills WHERE id=?`, id).Scan(&curName)
	if err != nil {
		return model.Skill{}, fmt.Errorf("skill not found")
	}
	if name != curName {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM skills WHERE name=? AND id<>?`, name, id).Scan(&n)
		if n > 0 {
			return model.Skill{}, fmt.Errorf("skill %q already exists", name)
		}
	}
	scope, pid, err := s.resolveScopeLocked(in.Scope, in.ProjectID)
	if err != nil {
		return model.Skill{}, err
	}
	now := Now()
	en := boolToInt(in.Enabled)
	_, err = s.db.Exec(`UPDATE skills SET name=?,description=?,content=?,category=?,tags=?,scope=?,project_id=?,enabled=?,updated_at=? WHERE id=?`,
		name, strings.TrimSpace(in.Description), in.Content, strings.TrimSpace(in.Category), strings.TrimSpace(in.Tags), scope, pid, en, now, id)
	if err != nil {
		return model.Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, err
	}
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, nil
}

func (s *Store) DeleteSkill(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM skills WHERE id=?`, id)
	return err
}

func (s *Store) SetEnabled(id string, enabled bool) (model.Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE skills SET enabled=?,updated_at=? WHERE id=?`, boolToInt(enabled), Now(), id)
	if err != nil {
		return model.Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, err
	}
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, err
}

// MoveSkill changes ONLY scope/project (sidebar drag-drop).
// scope "global" clears the project; scope "project" requires a valid project id.
func (s *Store) MoveSkill(id, scope, projectID string) (model.Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur string
	if err := s.db.QueryRow(`SELECT id FROM skills WHERE id=?`, id).Scan(&cur); err != nil {
		return model.Skill{}, fmt.Errorf("skill not found")
	}
	sc, pid, err := s.resolveScopeLocked(scope, projectID)
	if err != nil {
		return model.Skill{}, err
	}
	if _, err := s.db.Exec(`UPDATE skills SET scope=?,project_id=?,updated_at=? WHERE id=?`, sc, pid, Now(), id); err != nil {
		return model.Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, err
	}
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	return x, nil
}

// ---- projects ----

type ProjectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func (s *Store) ListProjects() []model.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,name,slug,description,color,created_at,updated_at FROM projects ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.CreatedAt, &p.UpdatedAt); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (s *Store) GetProject(id string) (model.Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p model.Project
	err := s.db.QueryRow(`SELECT id,name,slug,description,color,created_at,updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return model.Project{}, false
	}
	return p, true
}

func (s *Store) GetProjectBySlug(slug string) (model.Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := strings.ToLower(strings.TrimSpace(slug))
	var p model.Project
	err := s.db.QueryRow(`SELECT id,name,slug,description,color,created_at,updated_at FROM projects WHERE slug=?`, sl).
		Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return model.Project{}, false
	}
	return p, true
}

var projectColors = []string{"#10b981", "#6366f1", "#f59e0b", "#ec4899", "#06b6d4", "#8b5cf6"}

func (s *Store) CreateProject(in ProjectInput) (model.Project, error) {
	slug := NormalizeSlug(in.Slug)
	if slug == "" {
		slug = NormalizeSlug(in.Name)
	}
	if err := ValidateProject(in.Name, slug); err != nil {
		return model.Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := Now()
	p := model.Project{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(in.Name),
		Slug:        slug,
		Description: strings.TrimSpace(in.Description),
		Color:       strings.TrimSpace(in.Color),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if p.Color == "" {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&n)
		p.Color = projectColors[n%len(projectColors)]
	}
	_, err := s.db.Exec(`INSERT INTO projects(id,name,slug,description,color,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`, p.ID, p.Name, p.Slug, p.Description, p.Color, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Project{}, fmt.Errorf("project slug %q already exists", slug)
		}
		return model.Project{}, err
	}
	return p, nil
}

func (s *Store) UpdateProject(id string, in ProjectInput) (model.Project, error) {
	slug := NormalizeSlug(in.Slug)
	if slug == "" {
		slug = NormalizeSlug(in.Name)
	}
	if err := ValidateProject(in.Name, slug); err != nil {
		return model.Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur string
	if err := s.db.QueryRow(`SELECT id FROM projects WHERE id=?`, id).Scan(&cur); err != nil {
		return model.Project{}, fmt.Errorf("project not found")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE slug=? AND id<>?`, slug, id).Scan(&n)
	if n > 0 {
		return model.Project{}, fmt.Errorf("project slug %q already exists", slug)
	}
	_, err := s.db.Exec(`UPDATE projects SET name=?,slug=?,description=?,color=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(in.Name), slug, strings.TrimSpace(in.Description), strings.TrimSpace(in.Color), Now(), id)
	if err != nil {
		return model.Project{}, err
	}
	var p model.Project
	err = s.db.QueryRow(`SELECT id,name,slug,description,color,created_at,updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// DeleteProject keeps its skills by converting them to global
// (deleting knowledge silently would be worse than keeping it).
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`UPDATE skills SET scope='global',project_id='',updated_at=? WHERE project_id=?`, Now(), id)
	_, err := s.db.Exec(`DELETE FROM projects WHERE id=?`, id)
	return err
}

func (s *Store) CountProjectSkills(projectID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM skills WHERE project_id=?`, projectID).Scan(&n)
	return n
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- settings (appearance etc.) ----

func (s *Store) GetSettings() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			out[k] = v
		}
	}
	return out
}

func (s *Store) SetSetting(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
}

// ---- seed: the one default skill (global) ----

func (s *Store) seedDefaults() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM skills`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		// backfill scope for pre-scope rows
		_, _ = s.db.Exec(`UPDATE skills SET scope='global' WHERE scope IS NULL OR scope=''`)
		return nil
	}
	now := Now()
	_, err := s.db.Exec(`INSERT INTO skills(id,name,description,content,category,tags,scope,project_id,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), "git-commit", defaultGitCommitDescription, defaultGitCommitContent,
		"git", "git,commit,conventional-commits", "global", "", 1, now, now)
	return err
}

const defaultGitCommitDescription = "How to write Conventional Commits messages (feat/fix/docs…). Call this before creating any git commit message."

const defaultGitCommitContent = `# Conventional Commits skill

Use this skill whenever you need to write a git commit message.
Follow the Conventional Commits 1.0.0 spec.

## Format

` + "```" + `
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
` + "```" + `

## Types

- feat: a new feature
- fix: a bug fix
- docs: documentation only
- style: formatting, no code change
- refactor: neither fixes a bug nor adds a feature
- perf: performance improvement
- test: adding or fixing tests
- build: build system or dependencies
- ci: CI configuration
- chore: other changes
- revert: reverts a previous commit

## Rules

1. description: imperative, lowercase, no trailing period, max 72 chars.
   Good: ` + "`feat(auth): add oauth2 refresh token`" + `
   Bad: ` + "`Added stuff.`" + `
2. scope is optional but preferred when the change is bounded
   (e.g. auth, api, ui, db, mcp, skills).
3. body (optional): explain WHAT + WHY, not HOW. Wrap at 72 chars.
4. footer (optional): ` + "`BREAKING CHANGE: <what changed>`" + ` or ` + "`Refs: #123`" + `.
5. One logical change per commit. Never mix feat + fix.
6. Never use generic messages like "update", "fix bug", "wip".
7. Check ` + "`git status`" + ` and ` + "`git diff --staged`" + ` first so scope matches touched files.

## Examples

` + "```" + `
feat(skills): add enable toggle to skill cards

Lets users disable a skill without deleting it,
so its MCP tool disappears on next tools/list.

Refs: #12
` + "```" + `

` + "```" + `
fix(mcp): return 202 for JSON-RPC notifications

Strict clients (opencode) fail the handshake when
notifications get a 200 + error body.

BREAKING CHANGE: none
` + "```" + `

` + "```" + `
docs(readme): add opencode mcp install snippet
` + "```" + `

## Workflow for the agent

1. Run ` + "`git status --short`" + ` and ` + "`git diff --stat`" + `.
2. Pick type + scope from the diff.
3. Draft message following Format + Rules.
4. Keep subject <= 72 chars. Body lines <= 72 chars.
5. Output ONLY the commit message in a code block unless asked to commit.
`
