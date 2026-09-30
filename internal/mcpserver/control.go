// Control MCP: lets an AI agent manage the SkillsMCP app itself —
// workspaces, skills, projects, git sync, archives, or explain the app.
//
// Scopes beside this one:
//   - Personal (`SkillsMCP mcp`, skillsmcp): the default workspace's globals.
//   - Project (`SkillsMCP mcp --project <slug>`): main-workspace globals
//     + that project's skills.
//   - Workspace (`SkillsMCP mcp --workspace <slug> [--project <pslug>]`):
//     the same two shapes for another workspace
//     (skillsmcp-<workspace>, skillsmcp-<workspace>-<project>).
// Started explicitly with `SkillsMCP mcp --control` (skillsmcp-control).
// Kept separate so everyday sessions only see read tools, while a
// "manage my skills" session can opt into write tools.
package mcpserver

import (
	"fmt"
	"strings"

	"skillsmcp/internal/archive"
	"skillsmcp/internal/gitsync"
	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
)

// controlToolDefs lists the management tools. Read tools (list_skills,
// get_skill, list_projects, list_project_skills) are shared with the main
// MCP but run with control semantics (see callControlTool): every
// workspace, every scope, including disabled skills.
func controlToolDefs() []toolDef {
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	ws := strProp("Workspace slug, default main. Every skill/project tool accepts it.")
	return []toolDef{
		{
			Name:        "app_help",
			Description: "How to manage the SkillsMCP app: workspaces, MCP shapes, skill/project/git tools. Call this first in a management session.",
			InputSchema: emptyObj(),
		},
		{
			Name:        "create_skill",
			Description: "Create a new skill (goes live as an MCP tool instantly). Pick scope global (workspace-main MCP) or project (that project's MCP).",
			InputSchema: obj(map[string]any{
				"name":        strProp("Skill slug, e.g. deploy-api. Lowercase letters, digits, dashes."),
				"description": strProp("One line: when the AI should call this skill. Shown in tools/list."),
				"content":     strProp("Full markdown instruction returned to the AI when the skill runs."),
				"category":    strProp("Optional category, e.g. git, deploy."),
				"tags":        strProp("Optional comma-separated tags, e.g. api,deploy."),
				"scope":       strProp("global (default) or project."),
				"project":     strProp("Project slug. Required when scope=project, ignored otherwise."),
				"workspace":   ws,
			}, "name", "description", "content"),
		},
		{
			Name:        "update_skill",
			Description: "Edit an existing skill by name. Only the fields you pass change; the rest stay as-is. (Renaming is not supported — delete + create instead.)",
			InputSchema: obj(map[string]any{
				"name":        strProp("Current skill name, e.g. deploy-api."),
				"description": strProp("New one-line description."),
				"content":     strProp("New full markdown instruction."),
				"category":    strProp("New category."),
				"tags":        strProp("New comma-separated tags."),
				"workspace":   ws,
			}, "name"),
		},
		{
			Name:        "delete_skill",
			Description: "Permanently delete a skill by name. Its MCP tool disappears on the next tools/list.",
			InputSchema: obj(map[string]any{
				"name":      strProp("Skill name to delete."),
				"workspace": ws,
			}, "name"),
		},
		{
			Name:        "set_skill_enabled",
			Description: "Enable or disable a skill by name. Disabling hides its MCP tool without deleting it.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      strProp("Skill name."),
					"enabled":   map[string]any{"type": "boolean", "description": "true to enable, false to disable."},
					"workspace": ws,
				},
				"required": []string{"name", "enabled"},
			},
		},
		{
			Name:        "create_project",
			Description: "Create a project. It gets its own MCP serving workspace globals + its skills.",
			InputSchema: obj(map[string]any{
				"name":        strProp("Project display name, e.g. My App."),
				"slug":        strProp("Optional slug, e.g. my-app. Derived from the name when omitted."),
				"description": strProp("Optional project description."),
				"color":       strProp("Optional hex color for the UI, e.g. #6366f1."),
				"workspace":   ws,
			}, "name"),
		},
		{
			Name:        "update_project",
			Description: "Edit a project by slug. Only the fields you pass change (renaming slug is allowed via slug field).",
			InputSchema: obj(map[string]any{
				"project":     strProp("Current project slug, e.g. my-app."),
				"name":        strProp("New display name."),
				"slug":        strProp("New slug, e.g. my-app-v2."),
				"description": strProp("New description."),
				"color":       strProp("New hex color, e.g. #6366f1."),
				"workspace":   ws,
			}, "project"),
		},
		{
			Name:        "delete_project",
			Description: "Permanently delete a project by slug AND all skills inside it. Globals are kept. Its MCP stops working.",
			InputSchema: obj(map[string]any{
				"project":   strProp("Project slug to delete, e.g. my-app."),
				"workspace": ws,
			}, "project"),
		},
		{
			Name:        "export_skills",
			Description: "Export a workspace's globals (scope global) or one project (scope project + project slug) as a lossless archive manifest JSON: every skill with content, category, tags, enabled state and order, plus the project meta. Feed it back to import_skills to restore. The desktop UI downloads the same manifest wrapped in a .zip.",
			InputSchema: obj(map[string]any{
				"scope":     strProp("global (default) or project."),
				"project":   strProp("Project slug. Required when scope=project, ignored otherwise."),
				"workspace": ws,
			}),
		},
		{
			Name:        "import_skills",
			Description: "Restore an export_skills manifest JSON: recreates every skill exactly (content, category, tags, enabled, order). Project archives recreate their project when its slug is gone. Existing names are skipped, never overwritten.",
			InputSchema: obj(map[string]any{
				"archive":   strProp("The manifest JSON returned by export_skills."),
				"scope":     strProp("Optional override: global or project. Empty = the archive remembers."),
				"project":   strProp("Optional override: target project slug for project imports."),
				"workspace": strProp("Optional override: target workspace slug. Empty = main workspace."),
			}, "archive"),
		},
		{
			Name:        "list_workspaces",
			Description: "List workspaces (the personal main one plus team/shared ones): slug, git link, skill counts.",
			InputSchema: emptyObj(),
		},
		{
			Name:        "create_workspace",
			Description: "Create a workspace: its own globals + projects + git repo + MCPs (`mcp --workspace <slug>`).",
			InputSchema: obj(map[string]any{
				"name":        strProp("Workspace display name, e.g. Team Frontend."),
				"slug":        strProp("Optional slug, e.g. team-frontend. Derived from the name when omitted."),
				"description": strProp("Optional description."),
				"color":       strProp("Optional hex color, e.g. #6366f1."),
			}, "name"),
		},
		{
			Name:        "update_workspace",
			Description: "Edit a workspace by slug. Only the fields you pass change.",
			InputSchema: obj(map[string]any{
				"workspace":   strProp("Current workspace slug."),
				"name":        strProp("New display name."),
				"slug":        strProp("New slug."),
				"description": strProp("New description."),
				"color":       strProp("New hex color."),
			}, "workspace"),
		},
		{
			Name:        "delete_workspace",
			Description: "Permanently delete a workspace AND everything in it (projects + skills). The main workspace cannot be deleted.",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug to delete."),
			}, "workspace"),
		},
		{
			Name:        "set_workspace_git",
			Description: "Link a workspace to a git repo on any host (or unlink with an empty remote). No token needed when git on this machine is already authenticated (SSH keys/agent, credential manager, gh auth). Optional HTTPS token for machines where git isn't set up (stored server-side, never shown, sent per-command as a header — never embedded in the URL or written into the repo). Branch defaults to main.",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug."),
				"remote":    strProp("Git remote URL on any host, e.g. git@host:org/skills.git or https://host/org/skills.git. Empty unlinks."),
				"branch":    strProp("Branch, default main."),
				"token":     strProp("Optional HTTPS token for private repos on machines where git isn't authenticated. Empty keeps the stored one; changing remote without a token drops the old one."),
			}, "workspace"),
		},
		{
			Name:        "push_workspace",
			Description: "Write the workspace to its checkout, commit when dirty, and push to the linked remote. Manual sync — nothing pushes itself. force overwrites the remote (the fix when the repo has commits you don't have, e.g. a README init).",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug, default main."),
				"force":     map[string]any{"type": "boolean", "description": "Overwrite the remote with your library."},
			}),
		},
		{
			Name:        "pull_workspace",
			Description: "Pull the linked remote and restore skills into the workspace (missing projects recreated, existing names skipped).",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug, default main."),
			}),
		},
		{
			Name:        "workspace_status",
			Description: "One-line git sync state for a workspace: branch, clean/dirty, ahead/behind, unpublished changes.",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug, default main."),
			}),
		},
		{
			Name:        "workspace_conflicts",
			Description: "Compare a workspace's app library against its repo files: every skill (app vs repo entry + changed fields: description/content/category/tags/enabled), every project meta, workspace.json, and every file (manifests, skills/*.md, unrelated repo files). Merge with update_skill/create_skill/delete_skill per row, then push_workspace (force when histories diverged).",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug, default main."),
			}),
		},
		{
			Name:        "reset_workspace",
			Description: "Conflict fix, remote wins: discard the checkout, take the remote branch exactly, and restore it additively (existing skill names are skipped, so local-only skills survive). One click, no merging.",
			InputSchema: obj(map[string]any{
				"workspace": strProp("Workspace slug, default main."),
			}),
		},
		{
			Name:        "clone_workspace",
			Description: "Connect a repo on any host (public or private — system git auth such as SSH keys/agent, credential manager, gh auth; optional HTTPS token) as a new workspace and import all its skills. Each project in the repo keeps its own MCP.",
			InputSchema: obj(map[string]any{
				"name":      strProp("Display name for the new workspace."),
				"slug":      strProp("Optional slug. Derived from the name when omitted."),
				"remote":    strProp("Git URL to clone on any host, e.g. git@host:org/skills.git or https://host/org/skills.git."),
				"branch":    strProp("Branch, default main."),
				"token":     strProp("Optional HTTPS token (stored server-side, never shown) for machines where git isn't authenticated."),
			}, "name", "remote"),
		},
	}
}

// resolveWorkspace maps "" → main workspace.
func (s *Server) resolveWorkspace(slug string) (model.Workspace, error) {
	if strings.TrimSpace(slug) == "" {
		if w, ok := s.st.GetMainWorkspace(); ok {
			return w, nil
		}
		return model.Workspace{}, fmt.Errorf("no main workspace — the database may need migration")
	}
	if w, ok := s.st.GetWorkspaceBySlug(slug); ok {
		return w, nil
	}
	return model.Workspace{}, fmt.Errorf("unknown workspace %q — call list_workspaces first", slug)
}

// resolveProject finds a project by slug: inside the given workspace when
// set, otherwise by global lookup (which must be unambiguous).
func (s *Server) resolveProject(wsSlug, projSlug string) (model.Project, model.Workspace, error) {
	projSlug = strings.ToLower(strings.TrimSpace(projSlug))
	if projSlug == "" {
		return model.Project{}, model.Workspace{}, fmt.Errorf("project slug is required — call list_projects first")
	}
	if strings.TrimSpace(wsSlug) != "" {
		w, err := s.resolveWorkspace(wsSlug)
		if err != nil {
			return model.Project{}, model.Workspace{}, err
		}
		if p, ok := s.st.GetProjectBySlugIn(w.ID, projSlug); ok {
			return p, w, nil
		}
		return model.Project{}, model.Workspace{}, fmt.Errorf("unknown project %q in workspace %q", projSlug, w.Slug)
	}
	matches := s.st.FindProjectBySlug(projSlug)
	if len(matches) == 0 {
		return model.Project{}, model.Workspace{}, fmt.Errorf("unknown project %q — call list_projects first", projSlug)
	}
	if len(matches) > 1 {
		slugs := make([]string, 0, len(matches))
		for _, m := range matches {
			slugs = append(slugs, m.WorkspaceSlug)
		}
		return model.Project{}, model.Workspace{}, fmt.Errorf("project %q exists in workspaces %s — pass workspace to pick one", projSlug, strings.Join(slugs, ", "))
	}
	p := matches[0]
	w, ok := s.st.GetWorkspace(p.WorkspaceID)
	if !ok {
		return model.Project{}, model.Workspace{}, fmt.Errorf("project %q has no workspace", projSlug)
	}
	return p, w, nil
}

// lookupSkill finds a skill by name inside a workspace ("" = main).
func (s *Server) lookupSkill(wsSlug, name string) (model.Skill, model.Workspace, error) {
	w, err := s.resolveWorkspace(wsSlug)
	if err != nil {
		return model.Skill{}, model.Workspace{}, err
	}
	sk, ok := s.st.GetSkillIn(w.ID, name)
	if !ok {
		return model.Skill{}, model.Workspace{}, fmt.Errorf("unknown skill %q in workspace %q — call list_skills first", name, w.Slug)
	}
	return sk, w, nil
}

// callControlTool handles every tools/call on the control MCP.
func (s *Server) callControlTool(name string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	str := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	switch name {
	case "app_help":
		return map[string]any{"help": controlHelpText()}, nil
	case "list_skills":
		type row struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category,omitempty"`
			Tags        string `json:"tags,omitempty"`
			Scope       string `json:"scope,omitempty"`
			Project     string `json:"project,omitempty"`
			Workspace   string `json:"workspace,omitempty"`
			Enabled     bool   `json:"enabled"`
			UpdatedAt   string `json:"updatedAt,omitempty"`
		}
		rows := []row{}
		for _, sk := range s.st.ListSkills(true) {
			rows = append(rows, row{
				Name: sk.Name, Description: sk.Description,
				Category: sk.Category, Tags: sk.Tags,
				Scope: sScope(sk), Project: sk.ProjectSlug, Workspace: sk.WorkspaceSlug,
				Enabled: sk.Enabled, UpdatedAt: sk.UpdatedAt,
			})
		}
		return map[string]any{
			"skills": rows,
			"hint":   "Management view: every skill in every workspace and scope, including disabled ones. Use get_skill {name, workspace?} for full content, create_skill / update_skill / delete_skill / set_skill_enabled to manage them.",
		}, nil
	case "get_skill":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		sk, _, err := s.lookupSkill(str("workspace"), n)
		if err != nil {
			return nil, err
		}
		return skillPayload(sk), nil
	case "list_projects":
		type prow struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description,omitempty"`
			Workspace   string `json:"workspace,omitempty"`
			Skills      int    `json:"skills"`
		}
		rows := []prow{}
		for _, p := range s.st.ListProjects() {
			rows = append(rows, prow{Name: p.Name, Slug: p.Slug, Description: p.Description, Workspace: p.WorkspaceSlug, Skills: s.st.CountProjectSkills(p.ID)})
		}
		return map[string]any{
			"projects": rows,
			"hint":     "Each project has its own MCP serving its workspace globals + project skills: main-workspace projects via `mcp --project <slug>`, others via `mcp --workspace <ws> --project <slug>`.",
		}, nil
	case "list_project_skills":
		p, _, err := s.resolveProject(str("workspace"), str("project"))
		if err != nil {
			return nil, err
		}
		type row struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		rows := []row{}
		for _, sk := range s.st.ListProjectSkills(p.ID, true) {
			rows = append(rows, row{Name: sk.Name, Description: sk.Description})
		}
		return map[string]any{
			"project": p.Slug, "workspace": p.WorkspaceSlug, "skills": rows,
			"hint": "Switch to this project's MCP for full content, then list_skills.",
		}, nil
	case "create_skill":
		scope := strings.ToLower(strings.TrimSpace(str("scope")))
		if scope == "" {
			scope = "global"
		}
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		var pid string
		if scope == "project" {
			p, _, err := s.resolveProject(w.Slug, str("project"))
			if err != nil {
				return nil, err
			}
			pid = p.ID
		}
		sk, err := s.st.CreateSkill(store.SkillInput{
			Name:        str("name"),
			Description: str("description"),
			Content:     str("content"),
			Category:    str("category"),
			Tags:        str("tags"),
			Scope:       scope,
			ProjectID:   pid,
			WorkspaceID: w.ID,
			Enabled:     true,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"created": skillPayload(sk),
			"hint":    s.controlLiveHint(sk),
		}, nil
	case "update_skill":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		cur, w, err := s.lookupSkill(str("workspace"), n)
		if err != nil {
			return nil, err
		}
		in := store.SkillInput{
			Name: cur.Name, Description: cur.Description, Content: cur.Content,
			Category: cur.Category, Tags: cur.Tags,
			Scope: cur.Scope, ProjectID: cur.ProjectID, WorkspaceID: w.ID, Enabled: cur.Enabled,
		}
		if v := str("description"); v != "" {
			in.Description = v
		}
		if v := str("content"); v != "" {
			in.Content = v
		}
		if v, present := args["category"]; present {
			if vs, ok := v.(string); ok {
				in.Category = vs
			}
		}
		if v, present := args["tags"]; present {
			if vs, ok := v.(string); ok {
				in.Tags = vs
			}
		}
		sk, err := s.st.UpdateSkill(cur.ID, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"updated": skillPayload(sk),
			"hint":    s.controlLiveHint(sk),
		}, nil
	case "delete_skill":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		sk, _, err := s.lookupSkill(str("workspace"), n)
		if err != nil {
			return nil, err
		}
		if err := s.st.DeleteSkill(sk.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": sk.Name, "hint": "Its MCP tool disappears on the next tools/list."}, nil
	case "set_skill_enabled":
		n := str("name")
		if n == "" {
			return nil, fmt.Errorf("name required (e.g. {\"name\": \"git-commit\"})")
		}
		en, ok := args["enabled"].(bool)
		if !ok {
			return nil, fmt.Errorf("enabled required as a boolean (e.g. {\"name\": %q, \"enabled\": false})", n)
		}
		sk, _, err := s.lookupSkill(str("workspace"), n)
		if err != nil {
			return nil, err
		}
		updated, err := s.st.SetEnabled(sk.ID, en)
		if err != nil {
			return nil, err
		}
		state := "disabled — its MCP tool hides on the next tools/list"
		if en {
			state = "enabled — " + s.controlLiveHint(updated)
		}
		return map[string]any{"name": updated.Name, "enabled": updated.Enabled, "hint": state}, nil
	case "create_project":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		p, err := s.st.CreateProject(store.ProjectInput{
			Name:        str("name"),
			Slug:        str("slug"),
			Description: str("description"),
			Color:       str("color"),
			WorkspaceID: w.ID,
		})
		if err != nil {
			return nil, err
		}
		cmd := fmt.Sprintf("SkillsMCP mcp --project %s", p.Slug)
		if !w.IsMain {
			cmd = fmt.Sprintf("SkillsMCP mcp --workspace %s --project %s", w.Slug, p.Slug)
		}
		return map[string]any{
			"project": map[string]any{"name": p.Name, "slug": p.Slug, "description": p.Description, "workspace": w.Slug},
			"hint":    fmt.Sprintf("Its MCP is ready: `%s` serves workspace globals + %s skills. Create project skills with create_skill {scope: project, project: %q, workspace: %q}.", cmd, p.Slug, p.Slug, w.Slug),
		}, nil
	case "update_project":
		p, w, err := s.resolveProject(str("workspace"), str("project"))
		if err != nil {
			return nil, err
		}
		in := store.ProjectInput{Name: p.Name, Slug: p.Slug, Description: p.Description, Color: p.Color, WorkspaceID: w.ID}
		if v := str("name"); v != "" {
			in.Name = v
		}
		if v, present := args["slug"]; present {
			if vs, ok := v.(string); ok && strings.TrimSpace(vs) != "" {
				in.Slug = vs
			}
		}
		if v, present := args["description"]; present {
			if vs, ok := v.(string); ok {
				in.Description = vs
			}
		}
		if v, present := args["color"]; present {
			if vs, ok := v.(string); ok {
				in.Color = vs
			}
		}
		updated, err := s.st.UpdateProject(p.ID, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"project": map[string]any{"name": updated.Name, "slug": updated.Slug, "description": updated.Description, "workspace": w.Slug},
			"hint":    "Updated.",
		}, nil
	case "delete_project":
		p, w, err := s.resolveProject(str("workspace"), str("project"))
		if err != nil {
			return nil, err
		}
		n := s.st.CountProjectSkills(p.ID)
		if err := s.st.DeleteProject(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{
			"deleted": p.Slug, "workspace": w.Slug, "skillsDeleted": n,
			"hint": fmt.Sprintf("Project %q and its %d skill(s) are gone from workspace %q. Workspace globals kept.", p.Slug, n, w.Slug),
		}, nil
	case "export_skills":
		scope := strings.ToLower(strings.TrimSpace(str("scope")))
		if scope == "" {
			scope = "global"
		}
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		var pid string
		if scope == "project" {
			p, _, err := s.resolveProject(w.Slug, str("project"))
			if err != nil {
				return nil, err
			}
			pid = p.ID
		}
		m, err := archive.Export(s.st, w.ID, scope, pid)
		if err != nil {
			return nil, err
		}
		raw, err := m.Marshal()
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"archive":   string(raw),
			"workspace": w.Slug,
			"hint":      fmt.Sprintf("Lossless backup of %d skill(s) from workspace %q (%s). Restore with import_skills {archive: <this JSON>} — duplicates are skipped, project archives recreate their project.", len(m.Skills), w.Slug, m.Scope),
		}, nil
	case "import_skills":
		raw := str("archive")
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("archive is required — the manifest JSON from export_skills")
		}
		m, err := archive.Unmarshal([]byte(raw))
		if err != nil {
			return nil, err
		}
		scope := strings.ToLower(strings.TrimSpace(str("scope")))
		wsID := ""
		if wslug := strings.TrimSpace(str("workspace")); wslug != "" {
			w, err := s.resolveWorkspace(wslug)
			if err != nil {
				return nil, err
			}
			wsID = w.ID
		}
		var pid string
		if slug := strings.ToLower(strings.TrimSpace(str("project"))); slug != "" {
			// Target workspace wins when given, else the override slug's own workspace.
			wslug := str("workspace")
			if wslug == "" {
				if w, ok := s.st.GetMainWorkspace(); ok {
					wslug = w.Slug
				}
			}
			p, _, err := s.resolveProject(wslug, slug)
			if err != nil {
				return nil, err
			}
			pid = p.ID
			wsID = p.WorkspaceID
			if scope == "" {
				scope = "project"
			}
		}
		res, err := archive.Restore(s.st, m, scope, pid, wsID)
		if err != nil {
			return nil, err
		}
		skipped := res.Skipped
		if skipped == nil {
			skipped = []string{}
		}
		out := map[string]any{"imported": res.Imported, "skipped": skipped}
		if res.Project != nil {
			out["project"] = res.Project.Slug
			out["hint"] = fmt.Sprintf("Restored %d skill(s) into project %q (%d skipped as duplicates). Live now — no restart needed.", res.Imported, res.Project.Slug, len(skipped))
		} else {
			out["hint"] = fmt.Sprintf("Restored %d skill(s) (%d skipped as duplicates). Live now — no restart needed.", res.Imported, len(skipped))
		}
		return out, nil
	case "list_workspaces":
		type wrow struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description,omitempty"`
			Main        bool   `json:"main"`
			GitRemote   string `json:"gitRemote,omitempty"`
			GitBranch   string `json:"gitBranch,omitempty"`
			HasToken    bool   `json:"hasToken"`
			Skills      int    `json:"skills"`
			Projects    int    `json:"projects"`
		}
		rows := []wrow{}
		for _, w := range s.st.ListWorkspaces() {
			rows = append(rows, wrow{
				Name: w.Name, Slug: w.Slug, Description: w.Description, Main: w.IsMain,
				GitRemote: gitsync.RedactRemote(w.GitRemote), GitBranch: w.GitBranch,
				HasToken: w.HasToken,
				Skills:   s.st.CountWorkspaceSkills(w.ID),
				Projects: len(s.st.ListProjectsIn(w.ID)),
			})
		}
		return map[string]any{
			"workspaces": rows,
			"hint":       "Personal is the default workspace (`mcp`), others ride `mcp --workspace <slug>` with per-project MCPs. Manage with create/update/delete_workspace, sync with push/pull_workspace.",
		}, nil
	case "create_workspace":
		w, err := s.st.CreateWorkspace(store.WorkspaceInput{
			Name:        str("name"),
			Slug:        str("slug"),
			Description: str("description"),
			Color:       str("color"),
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"workspace": map[string]any{"name": w.Name, "slug": w.Slug},
			"hint":      fmt.Sprintf("Workspace ready. Its MCP: `SkillsMCP mcp --workspace %s` (globals), per project `SkillsMCP mcp --workspace %s --project <slug>`. Link a repo with set_workspace_git, sync with push_workspace.", w.Slug, w.Slug),
		}, nil
	case "update_workspace":
		wslug := strings.ToLower(strings.TrimSpace(str("workspace")))
		if wslug == "" {
			return nil, fmt.Errorf("workspace slug is required — call list_workspaces first")
		}
		cur, ok := s.st.GetWorkspaceBySlug(wslug)
		if !ok {
			return nil, fmt.Errorf("unknown workspace %q — call list_workspaces first", wslug)
		}
		in := store.WorkspaceInput{Name: cur.Name, Slug: cur.Slug, Description: cur.Description, Color: cur.Color, GitRemote: cur.GitRemote, GitBranch: cur.GitBranch}
		if v := str("name"); v != "" {
			in.Name = v
		}
		if v, present := args["slug"]; present {
			if vs, ok := v.(string); ok && strings.TrimSpace(vs) != "" {
				in.Slug = vs
			}
		}
		if v, present := args["description"]; present {
			if vs, ok := v.(string); ok {
				in.Description = vs
			}
		}
		if v, present := args["color"]; present {
			if vs, ok := v.(string); ok {
				in.Color = vs
			}
		}
		updated, err := s.st.UpdateWorkspace(cur.ID, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"workspace": map[string]any{"name": updated.Name, "slug": updated.Slug},
			"hint":      fmt.Sprintf("Updated. Its MCP: `SkillsMCP mcp --workspace %s`.", updated.Slug),
		}, nil
	case "delete_workspace":
		wslug := strings.ToLower(strings.TrimSpace(str("workspace")))
		if wslug == "" {
			return nil, fmt.Errorf("workspace slug is required — call list_workspaces first")
		}
		w, ok := s.st.GetWorkspaceBySlug(wslug)
		if !ok {
			return nil, fmt.Errorf("unknown workspace %q — call list_workspaces first", wslug)
		}
		n := s.st.CountWorkspaceSkills(w.ID)
		np := len(s.st.ListProjectsIn(w.ID))
		if err := s.st.DeleteWorkspace(w.ID); err != nil {
			return nil, err
		}
		return map[string]any{
			"deleted": w.Slug, "skillsDeleted": n, "projectsDeleted": np,
			"hint": fmt.Sprintf("Workspace %q, its %d project(s) and %d skill(s) are gone. Other workspaces untouched.", w.Slug, np, n),
		}, nil
	case "set_workspace_git":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		updated, err := s.st.SetWorkspaceGit(w.ID, str("remote"), str("branch"), str("token"))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(updated.GitRemote) == "" {
			return map[string]any{"workspace": w.Slug, "hint": "Unlinked (token dropped too) — the workspace is local-only now."}, nil
		}
		suffix := ""
		if updated.HasToken {
			suffix = " Token stored (never shown)."
		}
		return map[string]any{
			"workspace": map[string]any{"slug": updated.Slug, "branch": updated.GitBranch, "hasToken": updated.HasToken},
			"hint":      fmt.Sprintf("Linked %q to the repo (branch %s).%s Push to publish, pull to fetch.", updated.Slug, updated.GitBranch, suffix),
		}, nil
	case "push_workspace":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		force, _ := args["force"].(bool)
		res, err := gitsync.Push(s.st, w.ID, force)
		if err != nil {
			out := map[string]any{"workspace": w.Slug, "error": err.Error(), "detail": res.Detail, "committed": res.Committed}
			if res.Hint != "" {
				out["hint"] = res.Hint
			}
			return out, err
		}
		return map[string]any{
			"workspace": w.Slug, "committed": res.Committed, "pushed": res.Pushed,
			"forced": res.Forced, "skills": res.Skills, "hint": res.Detail,
		}, nil
	case "pull_workspace":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		res, err := gitsync.Pull(s.st, w.ID)
		if err != nil {
			out := map[string]any{"workspace": w.Slug, "error": err.Error()}
			if res.Hint != "" {
				out["hint"] = res.Hint
			}
			return out, err
		}
		skipped := res.Skipped
		if skipped == nil {
			skipped = []string{}
		}
		return map[string]any{
			"workspace": w.Slug, "imported": res.Imported, "skipped": skipped,
			"projects": res.Projects, "hint": res.Detail + " Live now — no restart needed.",
		}, nil
	case "reset_workspace":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		res, err := gitsync.Reset(s.st, w.ID)
		if err != nil {
			return map[string]any{"workspace": w.Slug, "error": err.Error()}, err
		}
		skipped := res.Skipped
		if skipped == nil {
			skipped = []string{}
		}
		return map[string]any{
			"workspace": w.Slug, "imported": res.Imported, "skipped": skipped,
			"projects": res.Projects, "hint": res.Detail + " Live now — no restart needed.",
		}, nil
	case "workspace_status":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		st, err := gitsync.StatusLine(s.st, w.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"workspace": w.Slug, "status": st}, nil
	case "workspace_conflicts":
		w, err := s.resolveWorkspace(str("workspace"))
		if err != nil {
			return nil, err
		}
		conf, err := gitsync.GetConflicts(s.st, w.ID)
		if err != nil {
			return map[string]any{"workspace": w.Slug, "error": err.Error()}, err
		}
		type skillRow struct {
			Name          string               `json:"name"`
			Scope         string               `json:"scope"`
			Project       string               `json:"project,omitempty"`
			Kind          string               `json:"kind"`
			ChangedFields []string             `json:"changedFields"`
			Local         *archive.SkillEntry  `json:"local,omitempty"`
			Remote        *archive.SkillEntry  `json:"remote,omitempty"`
			File          string               `json:"file"`
		}
		rows := []skillRow{}
		for _, sc := range conf.Skills {
			rows = append(rows, skillRow{
				Name: sc.Name, Scope: sc.Scope, Project: sc.ProjectSlug,
				Kind: sc.Kind, ChangedFields: sc.ChangedFields,
				Local: sc.Local, Remote: sc.Remote, File: sc.LocalFile,
			})
		}
		type fileRow struct {
			Path   string `json:"path"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		}
		files := []fileRow{}
		for _, f := range conf.Files {
			files = append(files, fileRow{Path: f.Path, Status: f.Status, Detail: f.Detail})
		}
		return map[string]any{
			"workspace": w.Slug, "branch": conf.Branch,
			"summary":   conf.Detail,
			"counts":    map[string]any{"local": conf.LocalSkills, "remote": conf.RemoteSkills, "addedLocal": conf.AddedLocal, "addedRemote": conf.AddedRemote, "modified": conf.Modified, "unchanged": conf.Unchanged},
			"skills":    rows,
			"files":     files,
			"hint":      "Merge row by row: kind added-remote → create_skill/import; added-local → keep (push publishes); modified → update_skill with the winning fields (or a hand-merged content). Then push_workspace (force: true when histories diverged). The desktop app shows the same data in Workspace → Compare / resolve with its markdown editor.",
		}, nil
	case "clone_workspace":
		ws, err := gitsync.CloneWorkspace(s.st, str("name"), str("slug"), str("remote"), str("branch"), str("token"))
		if err != nil {
			return nil, err
		}
		n := s.st.CountWorkspaceSkills(ws.ID)
		return map[string]any{
			"workspace": map[string]any{"name": ws.Name, "slug": ws.Slug},
			"skills":    n,
			"hint":      fmt.Sprintf("Connected: %d skill(s) imported. Its MCP: `SkillsMCP mcp --workspace %s`, per project add `--project <slug>`.", n, ws.Slug),
		}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q — call app_help to see what this MCP can do", name)
	}
}

// controlLiveHint tells the agent where a skill went live.
func (s *Server) controlLiveHint(sk model.Skill) string {
	if sScope(sk) == "global" || sk.ProjectID == "" {
		if w, ok := s.st.GetWorkspace(sk.WorkspaceID); ok && !w.IsMain {
			return fmt.Sprintf("Live now on that workspace's MCP: `SkillsMCP mcp --workspace %s` — no restart needed.", w.Slug)
		}
		return "Live now on the main MCP (globals) — no restart needed."
	}
	proj := sk.ProjectSlug
	if proj == "" {
		proj = "<slug>"
	}
	if w, ok := s.st.GetWorkspace(sk.WorkspaceID); ok && !w.IsMain {
		return fmt.Sprintf("Live now on that project's MCP: `SkillsMCP mcp --workspace %s --project %s` — no restart needed.", w.Slug, proj)
	}
	return fmt.Sprintf("Live now on that project's MCP: `SkillsMCP mcp --project %s` — no restart needed.", proj)
}

func controlHelpText() string {
	return "SkillsMCP control MCP (skillsmcp-control) — manage the app through me.\n\n" +
		"WORKSPACES + THE MCPS\n" +
		"- Personal workspace (default): `SkillsMCP mcp` (skillsmcp) serves its globals;\n" +
		"  each project rides `SkillsMCP mcp --project <slug>` (skillsmcp-<slug>).\n" +
		"- Other workspaces: `SkillsMCP mcp --workspace <slug>` (skillsmcp-<slug>)\n" +
		"  and per project `SkillsMCP mcp --workspace <slug> --project <pslug>`.\n" +
		"- Control (this one): manage everything across all workspaces.\n" +
		"  Most tools accept an optional workspace slug (default main).\n\n" +
		"MANAGING SKILLS\n" +
		"- Explore first: list_skills (every workspace, incl. disabled) →\n" +
		"  get_skill {name, workspace?} for full markdown.\n" +
		"- Create: create_skill {name, description, content, workspace?} for a\n" +
		"  global skill, or add scope: project + project: <slug>.\n" +
		"- Edit: update_skill {name, ...} changes only the fields you pass.\n" +
		"- Publish switch: set_skill_enabled {name, enabled} — disabling hides\n" +
		"  the tool without deleting the skill. Deleting is permanent.\n" +
		"- Backups: export_skills {scope, project?, workspace?} dumps a lossless\n" +
		"  archive — restore it with import_skills {archive}. Duplicates skip.\n" +
		"- Git sync (manual): set_workspace_git links a repo (SSH keys/agent,\n" +
		"  or HTTPS + private token stored server-side, never shown),\n" +
		"  push_workspace publishes (force: true overwrites a diverged repo),\n" +
		"  pull_workspace fetches, workspace_conflicts compares app vs repo\n" +
		"  skill-by-skill + file-by-file for merging, reset_workspace takes\n" +
		"  the repo's side with no merging, workspace_status shows the state.\n" +
		"  clone_workspace connects a repo as a new workspace and imports\n" +
		"  everything.\n" +
		"- Everything goes live instantly: tools/list is rebuilt from SQLite on every call.\n" +
		"- Skill names are unique per workspace, match [a-z0-9-_], max 64 chars,\n" +
		"  and must not collide with built-in tools.\n\n" +
		"HELPING THE USER WITH THE APP\n" +
		"- Skills live in ~/.skillsmcp/skills.db (override: SKILLSMCP_HOME). Delete it to reset.\n" +
		"- Workspace checkouts live in ~/.skillsmcp/workspaces/<slug>/repo.\n" +
		"- If the UI shows a blank page: quit every SkillsMCP process, then relaunch.\n" +
		"- If an MCP client can't connect: check the exe path in its config, restart the\n" +
		"  client session after config edits, and use the in-app Test MCP button."
}
