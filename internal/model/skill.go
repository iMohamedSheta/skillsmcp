// Package model defines Skills, Projects and their scopes.
//
// Scope rules:
//   - "global" skills (project_id = "") are served by the MAIN MCP
//     (`SkillsMCP mcp`) and are also included in every project MCP.
//   - "project" skills (project_id set) are served ONLY by that
//     project's MCP (`SkillsMCP mcp --project <slug>`).
package model

// Skill is one entry in the skill library.
// Name is the MCP tool name (e.g. "git-commit"): lowercase, [a-z0-9-_], max 64.
// Description is the tool description the AI reads to decide when to call it.
// Content is the full markdown instruction returned when the tool is called.
// Scope is "global" (project_id empty) or "project" (project_id set).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	Scope       string `json:"scope"`
	ProjectID   string `json:"projectId"`
	ProjectSlug string `json:"projectSlug"`
	ProjectName string `json:"projectName"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// SkillSummary is the lightweight index row for list_skills (no content).
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	Scope       string `json:"scope"`
	ProjectSlug string `json:"projectSlug"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updatedAt"`
}

// Project groups project-specific skills behind their own MCP:
// `SkillsMCP mcp --project <slug>` serves globals + that project's skills.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}
