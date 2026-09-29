export type SkillScope = 'global' | 'project';

export interface Skill {
  id: string;
  name: string;
  description: string;
  content: string;
  category: string;
  tags: string;
  scope: SkillScope | string;
  projectId: string;
  projectSlug: string;
  projectName: string;
  workspaceId: string;
  workspaceSlug: string;
  workspaceName: string;
  enabled: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
}

export interface SkillInput {
  name: string;
  description: string;
  content: string;
  category: string;
  tags: string;
  scope: string;
  projectId: string;
  workspaceId: string;
  enabled: boolean;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  workspaceId: string;
  workspaceSlug: string;
  workspaceName: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectInput {
  name: string;
  slug: string;
  description: string;
  color: string;
  workspaceId: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  gitRemote: string;
  gitBranch: string;
  hasToken: boolean;
  isMain: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceInput {
  name: string;
  slug: string;
  description: string;
  color: string;
  gitRemote: string;
  gitBranch: string;
  gitToken?: string;
}
