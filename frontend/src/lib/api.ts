import type { Project, ProjectInput, Skill, SkillInput } from './types';

// Wails binding shim.
// Inside the desktop app every call hits the REAL Go backend.
// The in-memory mocks below run ONLY in `npm run dev` (no window.go).
declare global {
  interface Window {
    go?: any;
  }
}

function binding(name: string, fallback: (...args: any[]) => Promise<any>): (...args: any[]) => Promise<any> {
  return async (...args: any[]) => {
    const fn = window.go?.main?.App?.[name];
    if (typeof fn === 'function') return await fn(...args);
    return await fallback(...args);
  };
}

const GIT_COMMIT = `# Conventional Commits skill

Use this skill whenever you need to write a git commit message.
Follow the Conventional Commits 1.0.0 spec.

## Format

\`<type>[optional scope]: <description>\`

## Types

feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

## Rules

1. imperative, lowercase, no trailing period, max 72 chars.
2. scope optional (auth, api, ui, db, mcp, skills).
3. One logical change per commit. Never "update" / "fix bug" / "wip".
`;

let memProjects: Project[] = [
  {
    id: 'p-demo', name: 'Demo App', slug: 'demo-app',
    description: 'Example project — its MCP serves globals + project skills.',
    color: '#6366f1',
    createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
  },
];

let mem: Skill[] = [
  {
    id: 'seed-git-commit',
    name: 'git-commit',
    description: 'How to write Conventional Commits messages (feat/fix/docs…). Call this before creating any git commit message.',
    content: GIT_COMMIT,
    category: 'git',
    tags: 'git,commit,conventional-commits',
    scope: 'global', projectId: '', projectSlug: '', projectName: '',
    enabled: true,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
  {
    id: 'seed-demo',
    name: 'demo-deploy',
    description: 'How to deploy the Demo App project.',
    content: '# Demo deploy\n\nProject-scoped example skill.',
    category: 'deploy',
    tags: 'demo',
    scope: 'project', projectId: 'p-demo', projectSlug: 'demo-app', projectName: 'Demo App',
    enabled: true,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

const rnd = (n = 8) => Math.random().toString(36).slice(2, 2 + n);
const normSlug = (s: string) => s.toLowerCase().trim().replace(/[\s_]+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '').slice(0, 64);

export const api = {
  Version: binding('Version', async (): Promise<string> => 'dev'),
  StorePath: binding('StorePath', async (): Promise<string> => '~/.skillsmcp/skills.db (SQLite WAL)'),
  ListSkills: binding('ListSkills', async (): Promise<Skill[]> => [...mem]),
  GetSkill: binding('GetSkill', async (name: string): Promise<Skill> => {
    const s = mem.find((x) => x.name === name);
    if (!s) throw new Error(`unknown skill "${name}"`);
    return s;
  }),
  CreateSkill: binding('CreateSkill', async (input: SkillInput): Promise<Skill> => {
    const name = input.name.toLowerCase().trim();
    if (mem.some((x) => x.name === name)) throw new Error(`skill "${name}" already exists`);
    const scope = input.scope === 'project' ? 'project' : 'global';
    const pid = scope === 'project' ? input.projectId : '';
    const proj = memProjects.find((p) => p.id === pid);
    const s: Skill = {
      ...input, name, scope, projectId: pid,
      projectSlug: proj?.slug || '', projectName: proj?.name || '',
      id: rnd(10), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), enabled: input.enabled ?? true,
    };
    mem.push(s);
    return s;
  }),
  UpdateSkill: binding('UpdateSkill', async (id: string, input: SkillInput): Promise<Skill> => {
    const i = mem.findIndex((x) => x.id === id);
    if (i < 0) throw new Error('skill not found');
    const scope = input.scope === 'project' ? 'project' : 'global';
    const pid = scope === 'project' ? input.projectId : '';
    const proj = memProjects.find((p) => p.id === pid);
    mem[i] = {
      ...mem[i], ...input, name: input.name.toLowerCase().trim(), scope, projectId: pid,
      projectSlug: proj?.slug || '', projectName: proj?.name || '',
      updatedAt: new Date().toISOString(),
    };
    return mem[i];
  }),
  DeleteSkill: binding('DeleteSkill', async (id: string): Promise<void> => {
    mem = mem.filter((x) => x.id !== id);
  }),
  SetSkillEnabled: binding('SetSkillEnabled', async (id: string, enabled: boolean): Promise<Skill> => {
    const s = mem.find((x) => x.id === id);
    if (!s) throw new Error('skill not found');
    s.enabled = enabled;
    s.updatedAt = new Date().toISOString();
    return s;
  }),
  MoveSkill: binding('MoveSkill', async (id: string, scope: string, projectId: string): Promise<Skill> => {
    const s = mem.find((x) => x.id === id);
    if (!s) throw new Error('skill not found');
    if (scope === 'project') {
      const p = memProjects.find((x) => x.id === projectId);
      if (!p) throw new Error('unknown project');
      s.scope = 'project'; s.projectId = p.id; s.projectSlug = p.slug; s.projectName = p.name;
    } else {
      s.scope = 'global'; s.projectId = ''; s.projectSlug = ''; s.projectName = '';
    }
    s.updatedAt = new Date().toISOString();
    return s;
  }),
  NormalizeName: binding('NormalizeName', async (raw: string): Promise<string> =>
    raw.toLowerCase().trim().replace(/[\s_]+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '').slice(0, 64)),
  NormalizeSlug: binding('NormalizeSlug', async (raw: string): Promise<string> => normSlug(raw)),
  ListProjects: binding('ListProjects', async (): Promise<Project[]> => [...memProjects]),
  CreateProject: binding('CreateProject', async (input: ProjectInput): Promise<Project> => {
    const slug = normSlug(input.slug || input.name);
    if (memProjects.some((p) => p.slug === slug)) throw new Error(`project slug "${slug}" already exists`);
    const p: Project = { ...input, slug, id: rnd(10), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
    memProjects.push(p);
    return p;
  }),
  UpdateProject: binding('UpdateProject', async (id: string, input: ProjectInput): Promise<Project> => {
    const i = memProjects.findIndex((p) => p.id === id);
    if (i < 0) throw new Error('project not found');
    memProjects[i] = { ...memProjects[i], ...input, slug: normSlug(input.slug || input.name), updatedAt: new Date().toISOString() };
    return memProjects[i];
  }),
  DeleteProject: binding('DeleteProject', async (id: string): Promise<void> => {
    memProjects = memProjects.filter((p) => p.id !== id);
    mem = mem.map((s) => (s.projectId === id ? { ...s, scope: 'global' as const, projectId: '', projectSlug: '', projectName: '' } : s));
  }),
  ProjectSkillCount: binding('ProjectSkillCount', async (id: string): Promise<number> => mem.filter((s) => s.projectId === id).length),
  GetSettings: binding('GetSettings', async (): Promise<Record<string, string>> => ({})),
  SetSetting: binding('SetSetting', async (): Promise<void> => {}),
  GetLogs: binding('GetLogs', async (): Promise<string[]> => ['(web-dev mock) skillsmcp started']),
  ClearLogs: binding('ClearLogs', async (): Promise<void> => {}),
  MCPStatus: binding('MCPStatus', async (): Promise<any> => ({ running: false, url: 'http://127.0.0.1:9423' })),
  StartMCP: binding('StartMCP', async (): Promise<string> => 'http://127.0.0.1:9423'),
  MCPToolsPreview: binding('MCPToolsPreview', async (): Promise<string> =>
    'list_skills · get_skill(name) · list_projects · ' + mem.filter((s) => s.enabled && s.scope === 'global').map((s) => s.name).join(' · ')),
  ProjectMCPToolsPreview: binding('ProjectMCPToolsPreview', async (slug: string): Promise<string> =>
    'list_skills · get_skill(name) · ' + mem.filter((s) => s.enabled && (s.scope === 'global' || s.projectSlug === slug)).map((s) => s.name).join(' · ')),
  OpencodeConfig: binding('OpencodeConfig', async (): Promise<string> => JSON.stringify({
    $schema: 'https://opencode.ai/config.json',
    mcp: { skillsmcp: { type: 'local', command: ['SkillsMCP.exe', 'mcp'], enabled: true } },
  }, null, 2)),
  OpencodeConfigForProject: binding('OpencodeConfigForProject', async (slug: string): Promise<string> => JSON.stringify({
    $schema: 'https://opencode.ai/config.json',
    mcp: { [`skillsmcp-${slug}`]: { type: 'local', command: ['SkillsMCP.exe', 'mcp', '--project', slug], enabled: true } },
  }, null, 2)),
  ClaudeConfig: binding('ClaudeConfig', async (): Promise<string> => JSON.stringify({
    mcpServers: { skillsmcp: { command: 'SkillsMCP.exe', args: ['mcp'] } },
  }, null, 2)),
  ClaudeConfigForProject: binding('ClaudeConfigForProject', async (slug: string): Promise<string> => JSON.stringify({
    mcpServers: { [`skillsmcp-${slug}`]: { command: 'SkillsMCP.exe', args: ['mcp', '--project', slug] } },
  }, null, 2)),
  TestMCP: binding('TestMCP', async (): Promise<string> =>
    'stdio msg 1 → {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",...}}\ntools/list → 200 (web-dev mock)'),
  TestProjectMCP: binding('TestProjectMCP', async (slug: string): Promise<string> => `(web-dev mock) project MCP skillsmcp-${slug} OK`),
};
