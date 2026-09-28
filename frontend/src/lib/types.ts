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
  enabled: boolean;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectInput {
  name: string;
  slug: string;
  description: string;
  color: string;
}
