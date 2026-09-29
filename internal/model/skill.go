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
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceSlug string `json:"workspaceSlug"`
	WorkspaceName string `json:"workspaceName"`
	Enabled     bool   `json:"enabled"`
	// SortOrder is the manual position inside its scope group
	// (globals / one project). Lower comes first; ties break by name.
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// SkillSummary is the lightweight index row for list_skills (no content).
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	Scope       string `json:"scope"`
	ProjectSlug string `json:"projectSlug"`
	WorkspaceSlug string `json:"workspaceSlug"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updatedAt"`
}

// Project groups project-specific skills behind their own MCP:
// `SkillsMCP mcp --project <slug>` serves globals + that project's skills.
// Every project lives in exactly one workspace.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceSlug string `json:"workspaceSlug"`
	WorkspaceName string `json:"workspaceName"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Workspace is a skill library of its own: workspace-global skills +
// projects, synced to its own git repo. The "main" workspace is the
// personal library (what single-workspace installs already have).
// Other workspaces get their own MCPs:
// `SkillsMCP mcp --workspace <slug>` and per project
// `SkillsMCP mcp --workspace <slug> --project <pslug>`.
type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	GitRemote   string `json:"gitRemote"`
	GitBranch   string `json:"gitBranch"`
	// HasToken reports whether a private-repo token is stored. The token
	// itself is never returned by any read — only used server-side.
	HasToken    bool   `json:"hasToken"`
	IsMain      bool   `json:"isMain"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}
