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
CREATE TABLE IF NOT EXISTS workspaces(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	slug TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '',
	git_remote TEXT NOT NULL DEFAULT '',
	git_branch TEXT NOT NULL DEFAULT '',
	git_token TEXT NOT NULL DEFAULT '',
	is_main INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS projects(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	slug TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '',
	workspace_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	UNIQUE(workspace_id, slug)
);
CREATE TABLE IF NOT EXISTS skills(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	tags TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL DEFAULT 'global',
	project_id TEXT NOT NULL DEFAULT '',
	workspace_id TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	UNIQUE(workspace_id, name)
);
CREATE TABLE IF NOT EXISTS settings(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

var indexStmts = []string{
	`CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects(slug)`,
	`CREATE INDEX IF NOT EXISTS idx_projects_workspace ON projects(workspace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_name ON skills(name)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_scope ON skills(scope)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_project ON skills(project_id)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_workspace ON skills(workspace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_workspaces_slug ON workspaces(slug)`,
}

// migrate runs in phases so old databases upgrade cleanly:
// 1. base tables (no-op when they already exist),
// 2. additive ALTERs for columns old tables lack (duplicate errors ignored),
// 3. workspace seed + backfill (single-workspace installs become "main"),
// 4. unique rebuilds (name/slug uniqueness becomes per-workspace),
// 5. indexes LAST — they reference the new columns and would abort the
//    whole migration with "no such column" if built before the ALTERs.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(baseTables); err != nil {
		return err
	}
	for _, col := range []string{
		`ALTER TABLE skills ADD COLUMN scope TEXT NOT NULL DEFAULT 'global'`,
		`ALTER TABLE skills ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE skills ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE skills ADD COLUMN workspace_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE projects ADD COLUMN workspace_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE workspaces ADD COLUMN git_token TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = s.db.Exec(col)
	}
	mainID := s.seedMainWorkspace()
	// The default workspace is called Personal (older installs seeded "Main").
	_, _ = s.db.Exec(`UPDATE workspaces SET name='Personal' WHERE is_main=1 AND name='Main'`)
	// Backfill every pre-workspace row into the main (personal) workspace.
	_, _ = s.db.Exec(`UPDATE skills SET workspace_id=? WHERE workspace_id IS NULL OR workspace_id=''`, mainID)
	_, _ = s.db.Exec(`UPDATE projects SET workspace_id=? WHERE workspace_id IS NULL OR workspace_id=''`, mainID)
	// Backfill order for pre-order databases (every row is still 0):
	// creation order (rowid). ReorderSkills always assigns >= 1,
	// so MAX = 0 reliably means "never ordered".
	var mx int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order),0) FROM skills`).Scan(&mx); err == nil && mx == 0 {
		_, _ = s.db.Exec(`UPDATE skills SET sort_order = rowid`)
	}
	if err := s.rebuildWorkspaceUniques(); err != nil {
		return err
	}
	for _, idx := range indexStmts {
		_, _ = s.db.Exec(idx)
	}
	return nil
}

// tableHasWorkspaceUnique reports whether a table already carries the
// composite UNIQUE(workspace_id, …) shape (fresh installs do).
func (s *Store) tableHasWorkspaceUnique(table string) bool {
	var sql string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&sql); err != nil {
		return true
	}
	return strings.Contains(sql, "UNIQUE(workspace_id")
}

// rebuildWorkspaceUniques converts pre-workspace installs (global
// UNIQUE(name) / UNIQUE(slug)) to per-workspace uniqueness by copying
// each table into the new shape. Data already backfilled above.
func (s *Store) rebuildWorkspaceUniques() error {
	if !s.tableHasWorkspaceUnique("skills") {
		if _, err := s.db.Exec(`CREATE TABLE skills_new(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT 'global',
			project_id TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			UNIQUE(workspace_id, name)
		)`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO skills_new(id,name,description,content,category,tags,scope,project_id,workspace_id,enabled,sort_order,created_at,updated_at)
			SELECT id,name,description,content,category,tags,scope,project_id,workspace_id,enabled,sort_order,created_at,updated_at FROM skills`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DROP TABLE skills`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`ALTER TABLE skills_new RENAME TO skills`); err != nil {
			return err
		}
	}
	if !s.tableHasWorkspaceUnique("projects") {
		if _, err := s.db.Exec(`CREATE TABLE projects_new(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			slug TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			color TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			UNIQUE(workspace_id, slug)
		)`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO projects_new(id,name,slug,description,color,workspace_id,created_at,updated_at)
			SELECT id,name,slug,description,color,workspace_id,created_at,updated_at FROM projects`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DROP TABLE projects`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`ALTER TABLE projects_new RENAME TO projects`); err != nil {
			return err
		}
	}
	return nil
}

// seedMainWorkspace ensures the personal workspace exists and returns its id.
func (s *Store) seedMainWorkspace() string {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM workspaces WHERE is_main=1`).Scan(&id); err == nil && id != "" {
		return id
	}
	// Adopt an existing "main" slug if the user made one by hand.
	if err := s.db.QueryRow(`SELECT id FROM workspaces WHERE slug='main'`).Scan(&id); err == nil && id != "" {
		_, _ = s.db.Exec(`UPDATE workspaces SET is_main=1 WHERE id=?`, id)
		return id
	}
	id = uuid.NewString()
	now := Now()
	_, _ = s.db.Exec(`INSERT INTO workspaces(id,name,slug,description,color,git_remote,git_branch,is_main,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, "Personal", "main", "Personal workspace — this install's original library.", "#10b981", "", "main", 1, now, now)
	return id
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]{0,63}$`)
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

var reserved = map[string]bool{
	"list_skills": true, "get_skill": true, "list_projects": true,
	"list_project_skills": true, "fleet_overview": true,
	"app_help": true, "create_skill": true, "update_skill": true,
	"delete_skill": true, "set_skill_enabled": true, "create_project": true,
	"update_project": true, "delete_project": true,
	"export_skills": true, "import_skills": true,
	"list_workspaces": true, "create_workspace": true, "update_workspace": true,
	"delete_workspace": true, "set_workspace_git": true,
	"push_workspace": true, "pull_workspace": true, "workspace_status": true,
	"workspace_conflicts": true,
	"clone_workspace": true, "reset_workspace": true,
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

const skillCols = `id,name,description,content,category,tags,scope,project_id,workspace_id,enabled,sort_order,created_at,updated_at`

// skillOrder keeps manual positions first, name as deterministic tiebreak.
const skillOrder = `ORDER BY sort_order, name`

func scanSkill(row interface {
	Scan(dest ...any) error
}) (model.Skill, error) {
	var x model.Skill
	var en int
	err := row.Scan(&x.ID, &x.Name, &x.Description, &x.Content, &x.Category, &x.Tags,
		&x.Scope, &x.ProjectID, &x.WorkspaceID, &en, &x.SortOrder, &x.CreatedAt, &x.UpdatedAt)
	x.Enabled = en == 1
	if x.Scope == "" {
		x.Scope = "global"
	}
	return x, err
}

// attachScope fills ProjectSlug/ProjectName + WorkspaceSlug/WorkspaceName
// for a batch (two small queries).
func (s *Store) attachScope(out []model.Skill) []model.Skill {
	return s.attachWorkspaces(s.attachProjects(out))
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
	return s.attachScope(out)
}

// attachWorkspaces fills WorkspaceSlug/WorkspaceName for a skill batch.
func (s *Store) attachWorkspaces(out []model.Skill) []model.Skill {
	if len(out) == 0 {
		return out
	}
	rows, err := s.db.Query(`SELECT id,slug,name FROM workspaces`)
	if err != nil {
		return out
	}
	defer rows.Close()
	byID := map[string]model.Workspace{}
	for rows.Next() {
		var w model.Workspace
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name); err == nil {
			byID[w.ID] = w
		}
	}
	for i := range out {
		if out[i].WorkspaceID != "" {
			if w, ok := byID[out[i].WorkspaceID]; ok {
				out[i].WorkspaceSlug = w.Slug
				out[i].WorkspaceName = w.Name
			}
		}
	}
	return out
}

// attachWorkspaceProjects fills WorkspaceSlug/WorkspaceName for projects.
func (s *Store) attachWorkspaceProjects(out []model.Project) []model.Project {
	if len(out) == 0 {
		return out
	}
	rows, err := s.db.Query(`SELECT id,slug,name FROM workspaces`)
	if err != nil {
		return out
	}
	defer rows.Close()
	byID := map[string]model.Workspace{}
	for rows.Next() {
		var w model.Workspace
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name); err == nil {
			byID[w.ID] = w
		}
	}
	for i := range out {
		if out[i].WorkspaceID != "" {
			if w, ok := byID[out[i].WorkspaceID]; ok {
				out[i].WorkspaceSlug = w.Slug
				out[i].WorkspaceName = w.Name
			}
		}
	}
	return out
}

// ListSkills returns ALL skills (every workspace, any scope).
// Management views use this; workspace MCPs use the In-variants.
func (s *Store) ListSkills(includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT ` + skillCols + ` FROM skills ` + skillOrder)
	}
	return s.querySkills(`SELECT ` + skillCols + ` FROM skills WHERE enabled=1 ` + skillOrder)
}

// ListSkillsIn returns one workspace's skills (any scope).
func (s *Store) ListSkillsIn(workspaceID string, includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? `+skillOrder, workspaceID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND enabled=1 `+skillOrder, workspaceID)
}

// mainWorkspaceID returns the personal workspace id (migrate seeds it).
func (s *Store) mainWorkspaceID() string {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM workspaces WHERE is_main=1`).Scan(&id); err == nil && id != "" {
		return id
	}
	return s.seedMainWorkspace()
}

// ListGlobalSkills returns enabled (or all) global skills for the MAIN MCP.
func (s *Store) ListGlobalSkills(includeDisabled bool) []model.Skill {
	return s.ListGlobalSkillsIn(s.mainWorkspaceID(), includeDisabled)
}

// ListGlobalSkillsIn returns one workspace's global skills.
func (s *Store) ListGlobalSkillsIn(workspaceID string, includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND (scope='global' OR scope='') `+skillOrder, workspaceID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND (scope='global' OR scope='') AND enabled=1 `+skillOrder, workspaceID)
}

// ListProjectSkills returns a single project's skills.
func (s *Store) ListProjectSkills(projectID string, includeDisabled bool) []model.Skill {
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE project_id=? `+skillOrder, projectID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE project_id=? AND enabled=1 `+skillOrder, projectID)
}

// ListProjectMCPSkills returns one workspace's globals + that project's skills
// (enabled only unless asked). The project must belong to the workspace.
func (s *Store) ListProjectMCPSkills(projectID string, includeDisabled bool) []model.Skill {
	var ws string
	_ = s.db.QueryRow(`SELECT workspace_id FROM projects WHERE id=?`, projectID).Scan(&ws)
	if includeDisabled {
		return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND ((scope='global' OR scope='') OR project_id=?) ORDER BY scope DESC, sort_order, name`, ws, projectID)
	}
	return s.querySkills(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND ((scope='global' OR scope='') OR project_id=?) AND enabled=1 ORDER BY scope DESC, sort_order, name`, ws, projectID)
}

// ListSummaries is the lightweight MAIN-MCP index: main-workspace globals.
func (s *Store) ListSummaries() []model.SkillSummary {
	return s.ListSummariesForProject("")
}

// ListSummariesForProject returns skill summaries; empty projectID means
// main-workspace globals only, otherwise globals plus that project.
func (s *Store) ListSummariesForProject(projectID string) []model.SkillSummary {
	return s.ListSummariesForProjectIn(s.mainWorkspaceID(), projectID)
}

// ListSummariesForProjectIn is the workspace-aware MCP index: one
// workspace's globals (+ that project when given).
func (s *Store) ListSummariesForProjectIn(workspaceID, projectID string) []model.SkillSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.db.Query(`SELECT name,description,category,tags,scope,project_id,enabled,updated_at FROM skills WHERE workspace_id=? AND (scope='global' OR scope='') AND enabled=1 ORDER BY sort_order, name`, workspaceID)
	} else {
		rows, err = s.db.Query(`SELECT name,description,category,tags,scope,project_id,enabled,updated_at FROM skills WHERE workspace_id=? AND ((scope='global' OR scope='') OR project_id=?) AND enabled=1 ORDER BY sort_order, name`, workspaceID, projectID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	// project + workspace slug lookups for the summary rows
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
	var wsSlug string
	_ = s.db.QueryRow(`SELECT slug FROM workspaces WHERE id=?`, workspaceID).Scan(&wsSlug)
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
			m.WorkspaceSlug = wsSlug
			out = append(out, m)
		}
	}
	return out
}

func (s *Store) GetSkill(name string) (model.Skill, bool) {
	return s.GetSkillIn(s.mainWorkspaceID(), name)
}

// GetSkillIn finds a skill by name inside one workspace.
func (s *Store) GetSkillIn(workspaceID, name string) (model.Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := strings.ToLower(strings.TrimSpace(name))
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE workspace_id=? AND name=?`, workspaceID, n)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, false
	}
	s.attachOneLocked(&x)
	return x, true
}

// attachOneLocked fills project + workspace slugs for a single skill.
// Callers must hold s.mu.
func (s *Store) attachOneLocked(x *model.Skill) {
	if x.ProjectID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM projects WHERE id=?`, x.ProjectID).Scan(&x.ProjectSlug, &x.ProjectName)
	}
	if x.WorkspaceID != "" {
		_ = s.db.QueryRow(`SELECT slug,name FROM workspaces WHERE id=?`, x.WorkspaceID).Scan(&x.WorkspaceSlug, &x.WorkspaceName)
	}
}

func (s *Store) GetSkillByID(id string) (model.Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, false
	}
	s.attachOneLocked(&x)
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
	WorkspaceID string `json:"workspaceId"`
	Enabled     bool   `json:"enabled"`
}

func normalizeScope(in string, projectID string) (string, string) {
	sc := strings.ToLower(strings.TrimSpace(in))
	if sc != "project" {
		return "global", ""
	}
	return "project", strings.TrimSpace(projectID)
}

func (s *Store) resolveScopeLocked(scope, projectID, workspaceID string) (string, string, string, error) {
	sc, pid := normalizeScope(scope, projectID)
	if sc == "project" {
		if pid == "" {
			return "", "", "", fmt.Errorf("project is required for project-scoped skills")
		}
		var ws string
		if err := s.db.QueryRow(`SELECT workspace_id FROM projects WHERE id=?`, pid).Scan(&ws); err != nil || ws == "" {
			return "", "", "", fmt.Errorf("unknown project — pick one from the Projects tab")
		}
		if workspaceID != "" && workspaceID != ws {
			return "", "", "", fmt.Errorf("that project lives in another workspace")
		}
		return sc, pid, ws, nil
	}
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		ws = s.mainWorkspaceID()
	} else {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id=?`, ws).Scan(&n); err != nil || n == 0 {
			return "", "", "", fmt.Errorf("unknown workspace")
		}
	}
	return sc, "", ws, nil
}

func (s *Store) CreateSkill(in SkillInput) (model.Skill, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if err := ValidateSkill(name, in.Description, in.Content); err != nil {
		return model.Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, pid, wsid, err := s.resolveScopeLocked(in.Scope, in.ProjectID, in.WorkspaceID)
	if err != nil {
		return model.Skill{}, err
	}
	now := Now()
	var mx int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(sort_order),0) FROM skills WHERE workspace_id=?`, wsid).Scan(&mx)
	x := model.Skill{
		ID:          uuid.NewString(),
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Content:     in.Content,
		Category:    strings.TrimSpace(in.Category),
		Tags:        strings.TrimSpace(in.Tags),
		Scope:       scope,
		ProjectID:   pid,
		WorkspaceID: wsid,
		Enabled:     true,
		SortOrder:   mx + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.db.Exec(`INSERT INTO skills(id,name,description,content,category,tags,scope,project_id,workspace_id,enabled,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.Name, x.Description, x.Content, x.Category, x.Tags, x.Scope, x.ProjectID, x.WorkspaceID, 1, x.SortOrder, x.CreatedAt, x.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Skill{}, fmt.Errorf("skill %q already exists in this workspace — pick another name or edit it", name)
		}
		return model.Skill{}, err
	}
	s.attachOneLocked(&x)
	return x, nil
}

func (s *Store) UpdateSkill(id string, in SkillInput) (model.Skill, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if err := ValidateSkill(name, in.Description, in.Content); err != nil {
		return model.Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var curName, curWS string
	err := s.db.QueryRow(`SELECT name,workspace_id FROM skills WHERE id=?`, id).Scan(&curName, &curWS)
	if err != nil {
		return model.Skill{}, fmt.Errorf("skill not found")
	}
	scope, pid, wsid, err := s.resolveScopeLocked(in.Scope, in.ProjectID, in.WorkspaceID)
	if err != nil {
		return model.Skill{}, err
	}
	if wsid == s.mainWorkspaceID() && curWS != "" && in.WorkspaceID == "" && pid == "" {
		// Editing without a workspace target keeps the skill where it is.
		wsid = curWS
	}
	if name != curName {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM skills WHERE workspace_id=? AND name=? AND id<>?`, wsid, name, id).Scan(&n)
		if n > 0 {
			return model.Skill{}, fmt.Errorf("skill %q already exists in this workspace", name)
		}
	}
	now := Now()
	en := boolToInt(in.Enabled)
	_, err = s.db.Exec(`UPDATE skills SET name=?,description=?,content=?,category=?,tags=?,scope=?,project_id=?,workspace_id=?,enabled=?,updated_at=? WHERE id=?`,
		name, strings.TrimSpace(in.Description), in.Content, strings.TrimSpace(in.Category), strings.TrimSpace(in.Tags), scope, pid, wsid, en, now, id)
	if err != nil {
		return model.Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, err
	}
	s.attachOneLocked(&x)
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
	s.attachOneLocked(&x)
	return x, err
}

// MoveSkill changes ONLY scope/project (sidebar drag-drop).
// scope "global" clears the project; scope "project" requires a valid project id.
// Dropping onto another workspace's group moves the skill there too: pass the
// target workspace via workspaceID (empty = keep, or the project’s workspace).
func (s *Store) MoveSkill(id, scope, projectID string) (model.Skill, error) {
	return s.MoveSkillTo(id, scope, projectID, "")
}

// MoveSkillTo is MoveSkill with an explicit target workspace.
func (s *Store) MoveSkillTo(id, scope, projectID, workspaceID string) (model.Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var curWS string
	if err := s.db.QueryRow(`SELECT workspace_id FROM skills WHERE id=?`, id).Scan(&curWS); err != nil {
		return model.Skill{}, fmt.Errorf("skill not found")
	}
	if workspaceID == "" {
		workspaceID = curWS
	}
	sc, pid, wsid, err := s.resolveScopeLocked(scope, projectID, workspaceID)
	if err != nil {
		return model.Skill{}, err
	}
	// A moved skill lands at the end of its new group.
	var mx int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(sort_order),0) FROM skills WHERE workspace_id=?`, wsid).Scan(&mx)
	if _, err := s.db.Exec(`UPDATE skills SET scope=?,project_id=?,workspace_id=?,sort_order=?,updated_at=? WHERE id=?`, sc, pid, wsid, mx+1, Now(), id); err != nil {
		return model.Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id=?`, id)
	x, err := scanSkill(row)
	if err != nil {
		return model.Skill{}, err
	}
	s.attachOneLocked(&x)
	return x, nil
}

// ReorderSkills persists a manual order: ids[0] first.
// Positions are 1-based; unknown ids are ignored.
func (s *Store) ReorderSkills(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if _, err := tx.Exec(`UPDATE skills SET sort_order=?,updated_at=? WHERE id=?`, i+1, Now(), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- projects (every project lives in exactly one workspace) ----

type ProjectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	WorkspaceID string `json:"workspaceId"`
}

const projectCols = `id,name,slug,description,color,workspace_id,created_at,updated_at`

func scanProject(row interface {
	Scan(dest ...any) error
}) (model.Project, error) {
	var p model.Project
	err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.WorkspaceID, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) queryProjects(q string, args ...any) []model.Project {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.WorkspaceID, &p.CreatedAt, &p.UpdatedAt); err == nil {
			out = append(out, p)
		}
	}
	return s.attachWorkspaceProjects(out)
}

// ListProjects returns every project (management view).
func (s *Store) ListProjects() []model.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryProjects(`SELECT ` + projectCols + ` FROM projects ORDER BY name`)
}

// ListProjectsIn returns one workspace's projects.
func (s *Store) ListProjectsIn(workspaceID string) []model.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryProjects(`SELECT `+projectCols+` FROM projects WHERE workspace_id=? ORDER BY name`, workspaceID)
}

func (s *Store) GetProject(id string) (model.Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if err != nil {
		return model.Project{}, false
	}
	return s.attachWorkspaceProjects([]model.Project{p})[0], true
}

// GetProjectBySlug finds a project by slug in the MAIN workspace.
func (s *Store) GetProjectBySlug(slug string) (model.Project, bool) {
	return s.GetProjectBySlugIn(s.mainWorkspaceID(), slug)
}

// GetProjectBySlugIn finds a project by slug inside one workspace.
func (s *Store) GetProjectBySlugIn(workspaceID, slug string) (model.Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := strings.ToLower(strings.TrimSpace(slug))
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE workspace_id=? AND slug=?`, workspaceID, sl))
	if err != nil {
		return model.Project{}, false
	}
	return s.attachWorkspaceProjects([]model.Project{p})[0], true
}

// FindProjectBySlug searches every workspace (control MCP disambiguation).
func (s *Store) FindProjectBySlug(slug string) []model.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := strings.ToLower(strings.TrimSpace(slug))
	return s.queryProjects(`SELECT `+projectCols+` FROM projects WHERE slug=? ORDER BY name`, sl)
}

var projectColors = []string{"#10b981", "#6366f1", "#f59e0b", "#ec4899", "#06b6d4", "#8b5cf6"}

// resolveWorkspaceLocked maps "" → main, validating explicit ids.
// Callers must hold s.mu.
func (s *Store) resolveWorkspaceLocked(workspaceID string) (string, error) {
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return s.mainWorkspaceID(), nil
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id=?`, ws).Scan(&n); err != nil || n == 0 {
		return "", fmt.Errorf("unknown workspace")
	}
	return ws, nil
}

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
	ws, err := s.resolveWorkspaceLocked(in.WorkspaceID)
	if err != nil {
		return model.Project{}, err
	}
	now := Now()
	p := model.Project{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(in.Name),
		Slug:        slug,
		Description: strings.TrimSpace(in.Description),
		Color:       strings.TrimSpace(in.Color),
		WorkspaceID: ws,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if p.Color == "" {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE workspace_id=?`, ws).Scan(&n)
		p.Color = projectColors[n%len(projectColors)]
	}
	_, err = s.db.Exec(`INSERT INTO projects(id,name,slug,description,color,workspace_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Slug, p.Description, p.Color, p.WorkspaceID, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Project{}, fmt.Errorf("project slug %q already exists in this workspace", slug)
		}
		return model.Project{}, err
	}
	return s.attachWorkspaceProjects([]model.Project{p})[0], nil
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
	cur, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if err != nil {
		return model.Project{}, fmt.Errorf("project not found")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE workspace_id=? AND slug=? AND id<>?`, cur.WorkspaceID, slug, id).Scan(&n)
	if n > 0 {
		return model.Project{}, fmt.Errorf("project slug %q already exists in this workspace", slug)
	}
	_, err = s.db.Exec(`UPDATE projects SET name=?,slug=?,description=?,color=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(in.Name), slug, strings.TrimSpace(in.Description), strings.TrimSpace(in.Color), Now(), id)
	if err != nil {
		return model.Project{}, err
	}
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if err != nil {
		return model.Project{}, err
	}
	return s.attachWorkspaceProjects([]model.Project{p})[0], nil
}

// DeleteProject deletes the project AND all skills inside it
// (a project is a hard boundary: its skills belong to it).
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM skills WHERE project_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
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

// ---- workspaces ----

type WorkspaceInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	GitRemote   string `json:"gitRemote"`
	GitBranch   string `json:"gitBranch"`
	// GitToken: "" = keep the stored token (or none); non-empty = replace it.
	GitToken string `json:"gitToken"`
}

const workspaceCols = `id,name,slug,description,color,git_remote,git_branch,git_token,is_main,created_at,updated_at`

func scanWorkspace(row interface {
	Scan(dest ...any) error
}) (model.Workspace, error) {
	var w model.Workspace
	var main int
	var token string
	err := row.Scan(&w.ID, &w.Name, &w.Slug, &w.Description, &w.Color, &w.GitRemote, &w.GitBranch, &token, &main, &w.CreatedAt, &w.UpdatedAt)
	w.IsMain = main == 1
	// The token itself never leaves the store layer flagged as readable:
	// callers see only whether one is stored. Server-side code that needs
	// it uses WorkspaceGitToken.
	w.HasToken = strings.TrimSpace(token) != ""
	return w, err
}

// WorkspaceGitToken returns the stored private-repo token ("" = none).
// Never exposed through bindings or MCP — gitsync only.
func (s *Store) WorkspaceGitToken(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t string
	_ = s.db.QueryRow(`SELECT git_token FROM workspaces WHERE id=?`, id).Scan(&t)
	return t
}

// ValidateWorkspace checks name/slug before save.
func ValidateWorkspace(name, slug string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("workspace name is required")
	}
	sl := strings.ToLower(strings.TrimSpace(slug))
	if sl == "" {
		return fmt.Errorf("workspace slug is required (e.g. team-frontend)")
	}
	if reserved[sl] {
		return fmt.Errorf("slug %q is reserved", sl)
	}
	if !slugRe.MatchString(sl) {
		return fmt.Errorf("slug must match [a-z0-9-], start alnum, max 64 (got %q)", slug)
	}
	return nil
}

var workspaceColors = []string{"#10b981", "#6366f1", "#f59e0b", "#ec4899", "#06b6d4", "#8b5cf6"}

func (s *Store) ListWorkspaces() []model.Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT ` + workspaceCols + ` FROM workspaces ORDER BY is_main DESC, name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Workspace
	for rows.Next() {
		if w, err := scanWorkspace(rows); err == nil {
			out = append(out, w)
		}
	}
	return out
}

// GetMainWorkspace returns the personal workspace.
func (s *Store) GetMainWorkspace() (model.Workspace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE is_main=1`))
	if err != nil {
		return model.Workspace{}, false
	}
	return w, true
}

func (s *Store) GetWorkspace(id string) (model.Workspace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id=?`, id))
	if err != nil {
		return model.Workspace{}, false
	}
	return w, true
}

func (s *Store) GetWorkspaceBySlug(slug string) (model.Workspace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE slug=?`, strings.ToLower(strings.TrimSpace(slug))))
	if err != nil {
		return model.Workspace{}, false
	}
	return w, true
}

func (s *Store) CreateWorkspace(in WorkspaceInput) (model.Workspace, error) {
	slug := NormalizeSlug(in.Slug)
	if slug == "" {
		slug = NormalizeSlug(in.Name)
	}
	if err := ValidateWorkspace(in.Name, slug); err != nil {
		return model.Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := Now()
	w := model.Workspace{
		ID: slugID(), Name: strings.TrimSpace(in.Name), Slug: slug,
		Description: strings.TrimSpace(in.Description), Color: strings.TrimSpace(in.Color),
		GitRemote: strings.TrimSpace(in.GitRemote), GitBranch: strings.TrimSpace(in.GitBranch),
		CreatedAt: now, UpdatedAt: now,
	}
	if w.GitBranch == "" {
		w.GitBranch = "main"
	}
	if w.Color == "" {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM workspaces`).Scan(&n)
		w.Color = workspaceColors[n%len(workspaceColors)]
	}
	_, err := s.db.Exec(`INSERT INTO workspaces(id,name,slug,description,color,git_remote,git_branch,git_token,is_main,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.Name, w.Slug, w.Description, w.Color, w.GitRemote, w.GitBranch, strings.TrimSpace(in.GitToken), 0, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Workspace{}, fmt.Errorf("workspace slug %q already exists", slug)
		}
		return model.Workspace{}, err
	}
	w.HasToken = strings.TrimSpace(in.GitToken) != ""
	return w, nil
}

func slugID() string { return uuid.NewString() }

func (s *Store) UpdateWorkspace(id string, in WorkspaceInput) (model.Workspace, error) {
	slug := NormalizeSlug(in.Slug)
	if slug == "" {
		slug = NormalizeSlug(in.Name)
	}
	if err := ValidateWorkspace(in.Name, slug); err != nil {
		return model.Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id=?`, id))
	if err != nil {
		return model.Workspace{}, fmt.Errorf("workspace not found")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE slug=? AND id<>?`, slug, id).Scan(&n)
	if n > 0 {
		return model.Workspace{}, fmt.Errorf("workspace slug %q already exists", slug)
	}
	// Empty token input keeps the stored one; non-empty replaces it.
	var curToken string
	_ = s.db.QueryRow(`SELECT git_token FROM workspaces WHERE id=?`, id).Scan(&curToken)
	newToken := curToken
	if strings.TrimSpace(in.GitToken) != "" {
		newToken = strings.TrimSpace(in.GitToken)
	}
	_, err = s.db.Exec(`UPDATE workspaces SET name=?,slug=?,description=?,color=?,git_remote=?,git_branch=?,git_token=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(in.Name), slug, strings.TrimSpace(in.Description), strings.TrimSpace(in.Color),
		strings.TrimSpace(in.GitRemote), strings.TrimSpace(in.GitBranch), newToken, Now(), id)
	if err != nil {
		return model.Workspace{}, err
	}
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id=?`, id))
	if err != nil {
		return model.Workspace{}, err
	}
	w.IsMain = cur.IsMain
	return w, nil
}

// SetWorkspaceGit links a workspace to a git remote (empty remote unlinks
// and drops the stored token). Token rules: non-empty replaces the stored
// token; empty keeps it — except when the remote itself changes or is
// removed, which drops the old token rather than leaking it elsewhere.
func (s *Store) SetWorkspaceGit(id, remote, branch, token string) (model.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id=?`, id))
	if err != nil {
		return model.Workspace{}, fmt.Errorf("workspace not found")
	}
	var curToken string
	_ = s.db.QueryRow(`SELECT git_token FROM workspaces WHERE id=?`, id).Scan(&curToken)
	newRemote := strings.TrimSpace(remote)
	newToken := curToken
	if strings.TrimSpace(token) != "" {
		newToken = strings.TrimSpace(token)
	} else if newRemote != strings.TrimSpace(cur.GitRemote) {
		newToken = ""
	}
	if newRemote == "" {
		newToken = ""
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	if _, err := s.db.Exec(`UPDATE workspaces SET git_remote=?,git_branch=?,git_token=?,updated_at=? WHERE id=?`,
		newRemote, branch, newToken, Now(), id); err != nil {
		return model.Workspace{}, err
	}
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id=?`, id))
	return w, err
}

// DeleteWorkspace deletes a workspace AND everything in it (projects +
// skills). The main workspace cannot be deleted. Globals of other
// workspaces are untouched.
func (s *Store) DeleteWorkspace(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var isMain int
	if err := s.db.QueryRow(`SELECT is_main FROM workspaces WHERE id=?`, id).Scan(&isMain); err != nil {
		return fmt.Errorf("workspace not found")
	}
	if isMain == 1 {
		return fmt.Errorf("the Personal workspace cannot be deleted")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM skills WHERE workspace_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE workspace_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM workspaces WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CountWorkspaceSkills reports globals + project skills in a workspace.
func (s *Store) CountWorkspaceSkills(workspaceID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM skills WHERE workspace_id=?`, workspaceID).Scan(&n)
	return n
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
	_, err := s.db.Exec(`INSERT INTO skills(id,name,description,content,category,tags,scope,project_id,workspace_id,enabled,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), "git-commit", defaultGitCommitDescription, defaultGitCommitContent,
		"git", "git,commit,conventional-commits", "global", "", s.mainWorkspaceID(), 1, 1, now, now)
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
