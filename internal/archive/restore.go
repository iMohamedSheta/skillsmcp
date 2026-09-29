// Restore imports a Manifest into a store without modifications:
// every skill comes back with name, description, content, category,
// tags, enabled state and relative order intact.
//
// Target resolution:
//   - workspace: explicit targetWorkspaceID wins; otherwise the main
//     workspace (project archives from another workspace still land
//     here unless redirected — pass targetWorkspaceID to keep them home).
//   - project scope: explicit targetProjectID wins; else the project
//     with the manifest's slug inside the target workspace if it
//     exists, otherwise recreated from the manifest meta.
// Existing skill names (per workspace) are skipped, never overwritten.
package archive

import (
	"fmt"
	"strings"

	"skillsmcp/internal/model"
	"skillsmcp/internal/store"
)

// Result summarizes one import.
type Result struct {
	Imported int          `json:"imported"`
	Skipped  []string     `json:"skipped"`
	Project  *ProjectMeta `json:"project,omitempty"`
}

// Restore writes a manifest into st.
func Restore(st *store.Store, m Manifest, targetScope, targetProjectID, targetWorkspaceID string) (Result, error) {
	res := Result{Skipped: []string{}}
	wsID := strings.TrimSpace(targetWorkspaceID)
	if wsID == "" {
		if w, ok := st.GetMainWorkspace(); ok {
			wsID = w.ID
		} else {
			return res, fmt.Errorf("no workspace available")
		}
	} else if _, ok := st.GetWorkspace(wsID); !ok {
		return res, fmt.Errorf("unknown workspace")
	}
	scope := strings.ToLower(strings.TrimSpace(targetScope))
	if scope == "" {
		scope = m.Scope
	}
	var pid string
	var meta *ProjectMeta
	if scope == "project" {
		pid = strings.TrimSpace(targetProjectID)
		if pid == "" && m.Project != nil && m.Project.Slug != "" {
			if p, ok := st.GetProjectBySlugIn(wsID, m.Project.Slug); ok {
				pid = p.ID
			} else {
				created, err := st.CreateProject(store.ProjectInput{
					Name: m.Project.Name, Slug: m.Project.Slug,
					Description: m.Project.Description, Color: m.Project.Color,
					WorkspaceID: wsID,
				})
				if err != nil {
					return res, fmt.Errorf("recreate project %q: %v", m.Project.Slug, err)
				}
				pid = created.ID
			}
		}
		if pid == "" {
			return res, fmt.Errorf("no target project: pick one or import a project archive carrying its project")
		}
		p, ok := st.GetProject(pid)
		if !ok || p.WorkspaceID != wsID {
			return res, fmt.Errorf("target project is not in the target workspace")
		}
		meta = &ProjectMeta{Name: p.Name, Slug: p.Slug, Description: p.Description, Color: p.Color}
		scope = "project"
	} else {
		scope = "global"
	}
	res.Project = meta

	inScope := scope
	for _, e := range m.Skills {
		name := strings.ToLower(strings.TrimSpace(e.Name))
		if _, exists := st.GetSkillIn(wsID, name); exists {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		// Normalize the entry scope: a global archive redirected into a
		// project (or vice versa) lands wholly in the target scope.
		sk, err := st.CreateSkill(store.SkillInput{
			Name: name, Description: strings.TrimSpace(e.Description), Content: e.Content,
			Category: strings.TrimSpace(e.Category), Tags: strings.TrimSpace(e.Tags),
			Scope: inScope, ProjectID: pid, WorkspaceID: wsID, Enabled: true,
		})
		if err != nil {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		if !e.Enabled {
			if _, err := st.SetEnabled(sk.ID, false); err != nil {
				res.Skipped = append(res.Skipped, name)
				continue
			}
		}
		res.Imported++
	}
	return res, nil
}

// Export collects the manifest for a scope inside one workspace:
// "global" → all global skills (including disabled); "project" +
// projectID → that project's skills (including disabled) plus meta.
// Empty workspaceID means the main workspace.
func Export(st *store.Store, workspaceID, scope, projectID string) (Manifest, error) {
	wsID := strings.TrimSpace(workspaceID)
	if wsID == "" {
		if w, ok := st.GetMainWorkspace(); ok {
			wsID = w.ID
		} else {
			return Manifest{}, fmt.Errorf("no workspace available")
		}
	}
	if strings.ToLower(strings.TrimSpace(scope)) == "project" {
		p, ok := st.GetProject(strings.TrimSpace(projectID))
		if !ok || p.WorkspaceID != wsID {
			return Manifest{}, fmt.Errorf("unknown project in this workspace")
		}
		mp := model.Project{
			ID: p.ID, Name: p.Name, Slug: p.Slug,
			Description: p.Description, Color: p.Color,
		}
		return FromSkills(st.ListProjectSkills(p.ID, true), &mp), nil
	}
	return FromSkills(st.ListGlobalSkillsIn(wsID, true), nil), nil
}
