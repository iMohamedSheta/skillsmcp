import type { Project, ProjectInput, Skill, SkillInput, Workspace, WorkspaceInput } from './types';

export interface UpdateInfo {
  currentVersion: string;
  latestVersion: string;
  releaseName: string;
  notes: string;
  pageUrl: string;
  assetName: string;
  downloadUrl: string;
  size: number;
  publishedAt: string;
  updateAvailable: boolean;
  canInstall: boolean;
  platform: string;
}

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

let memWorkspaces: Workspace[] = [
  {
    id: 'w-main', name: 'Main', slug: 'main',
    description: 'Personal workspace.',
    color: '#10b981', gitRemote: '', gitBranch: 'main', hasToken: false, isMain: true,
    createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
  },
];

let memProjects: Project[] = [
  {
    id: 'p-demo', name: 'Demo App', slug: 'demo-app',
    description: 'Example project — its MCP serves globals + project skills.',
    color: '#6366f1',
    workspaceId: 'w-main', workspaceSlug: 'main', workspaceName: 'Main',
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
    workspaceId: 'w-main', workspaceSlug: 'main', workspaceName: 'Main',
    enabled: true,
    sortOrder: 1,
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
    workspaceId: 'w-main', workspaceSlug: 'main', workspaceName: 'Main',
    enabled: true,
    sortOrder: 2,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

const rnd = (n = 8) => Math.random().toString(36).slice(2, 2 + n);
const normSlug = (s: string) => s.toLowerCase().trim().replace(/[\s_]+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '').slice(0, 64);

export const api = {
  Version: binding('Version', async (): Promise<string> => 'dev'),
  StorePath: binding('StorePath', async (): Promise<string> => '~/.skillsmcp/skills.db (SQLite WAL)'),
  ListSkills: binding('ListSkills', async (): Promise<Skill[]> =>
    [...mem].sort((a, b) => (a.sortOrder - b.sortOrder) || a.name.localeCompare(b.name))),
  GetSkill: binding('GetSkill', async (name: string): Promise<Skill> => {
    const s = mem.find((x) => x.name === name);
    if (!s) throw new Error(`unknown skill "${name}"`);
    return s;
  }),
  CreateSkill: binding('CreateSkill', async (input: SkillInput): Promise<Skill> => {
    const name = input.name.toLowerCase().trim();
    const scope = input.scope === 'project' ? 'project' : 'global';
    const pid = scope === 'project' ? input.projectId : '';
    const proj = memProjects.find((p) => p.id === pid);
    const wsid = proj ? proj.workspaceId : (input.workspaceId || 'w-main');
    const ws = memWorkspaces.find((w) => w.id === wsid) || memWorkspaces[0];
    if (mem.some((x) => x.name === name && x.workspaceId === wsid)) throw new Error(`skill "${name}" already exists in this workspace`);
    const mx = mem.filter((x) => x.workspaceId === wsid).reduce((m, x) => Math.max(m, x.sortOrder || 0), 0);
    const s: Skill = {
      ...input, name, scope, projectId: pid,
      projectSlug: proj?.slug || '', projectName: proj?.name || '',
      workspaceId: wsid, workspaceSlug: ws?.slug || '', workspaceName: ws?.name || '',
      id: rnd(10), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), enabled: input.enabled ?? true,
      sortOrder: mx + 1,
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
    const wsid = proj ? proj.workspaceId : (input.workspaceId || mem[i].workspaceId);
    const ws = memWorkspaces.find((w) => w.id === wsid);
    mem[i] = {
      ...mem[i], ...input, name: input.name.toLowerCase().trim(), scope, projectId: pid,
      projectSlug: proj?.slug || '', projectName: proj?.name || '',
      workspaceId: wsid, workspaceSlug: ws?.slug || '', workspaceName: ws?.name || '',
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
    const mx = mem.reduce((m, x) => Math.max(m, x.sortOrder || 0), 0);
    s.sortOrder = mx + 1;
    s.updatedAt = new Date().toISOString();
    return s;
  }),
  ReorderSkills: binding('ReorderSkills', async (ids: string[]): Promise<void> => {
    const pos = new Map(ids.map((id, i) => [id, i + 1]));
    for (const s of mem) {
      const p = pos.get(s.id);
      if (p !== undefined) {
        s.sortOrder = p;
        s.updatedAt = new Date().toISOString();
      }
    }
  }),
  NormalizeName: binding('NormalizeName', async (raw: string): Promise<string> =>
    raw.toLowerCase().trim().replace(/[\s_]+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '').slice(0, 64)),
  NormalizeSlug: binding('NormalizeSlug', async (raw: string): Promise<string> => normSlug(raw)),
  ListProjects: binding('ListProjects', async (): Promise<Project[]> => [...memProjects]),
  ListProjectsIn: binding('ListProjectsIn', async (workspaceId: string): Promise<Project[]> =>
    memProjects.filter((p) => !workspaceId || p.workspaceId === workspaceId)),
  ListSkillsIn: binding('ListSkillsIn', async (workspaceId: string): Promise<Skill[]> =>
    mem.filter((s) => !workspaceId || s.workspaceId === workspaceId)
      .sort((a, b) => (a.sortOrder - b.sortOrder) || a.name.localeCompare(b.name))),
  CreateProject: binding('CreateProject', async (input: ProjectInput): Promise<Project> => {
    const slug = normSlug(input.slug || input.name);
    const wsid = input.workspaceId || 'w-main';
    const ws = memWorkspaces.find((w) => w.id === wsid) || memWorkspaces[0];
    if (memProjects.some((p) => p.slug === slug && p.workspaceId === wsid)) throw new Error(`project slug "${slug}" already exists in this workspace`);
    const p: Project = { ...input, slug, id: rnd(10), workspaceId: wsid, workspaceSlug: ws?.slug || '', workspaceName: ws?.name || '', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
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
    mem = mem.filter((s) => s.projectId !== id);
  }),
  ExportArchive: binding('ExportArchive', async (scope: string, projectId: string, _workspaceId: string): Promise<{ filename: string; base64: string; skills: string }> => {
    const list = scope === 'project' ? mem.filter((s) => s.projectId === projectId) : mem.filter((s) => s.scope !== 'project');
    const manifest = {
      format: 'skillsmcp-archive/v1', exportedAt: new Date().toISOString(), scope: scope === 'project' ? 'project' : 'global',
      project: scope === 'project' ? (() => { const p = memProjects.find((x) => x.id === projectId); return p ? { name: p.name, slug: p.slug, description: p.description, color: p.color } : undefined; })() : undefined,
      skills: list.map((s) => ({ name: s.name, description: s.description, content: s.content, category: s.category, tags: s.tags, enabled: s.enabled, sortOrder: s.sortOrder })),
    };
    const json = JSON.stringify(manifest, null, 2);
    return { filename: `skillsmcp-export-${Date.now()}.json`, base64: btoa(unescape(encodeURIComponent(json))), skills: String(list.length) };
  }),
  ImportArchive: binding('ImportArchive', async (base64zip: string, targetScope: string, targetProjectId: string, _targetWorkspaceId: string): Promise<{ imported: number; skipped: string[]; scope: string; project?: string }> => {
    // Web-dev mock speaks manifest JSON (the desktop backend also eats .zip).
    const json = decodeURIComponent(escape(atob(base64zip)));
    const m = JSON.parse(json);
    if (!m || !Array.isArray(m.skills)) throw new Error('not a skills archive');
    let pid = '';
    let wsid = _targetWorkspaceId || 'w-main';
    if ((targetScope || m.scope) === 'project') {
      pid = targetProjectId || '';
      if (!pid && m.project?.slug) {
        let p: Project | undefined = memProjects.find((x) => x.slug === m.project.slug && x.workspaceId === wsid);
        if (!p) {
          const ws = memWorkspaces.find((w) => w.id === wsid) || memWorkspaces[0];
          p = { id: rnd(10), name: m.project.name || m.project.slug, slug: m.project.slug, description: m.project.description || '', color: m.project.color || '#6366f1', workspaceId: wsid, workspaceSlug: ws?.slug || '', workspaceName: ws?.name || '', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
          memProjects.push(p);
        }
        pid = (p as Project).id;
      }
      if (!pid) throw new Error('no target project');
    }
    let imported = 0;
    const skipped: string[] = [];
    for (const e of m.skills) {
      const name = String(e.name || '').toLowerCase().trim();
      if (!name || mem.some((x) => x.name === name && x.workspaceId === wsid)) { if (name) skipped.push(name); continue; }
      const proj = memProjects.find((p) => p.id === pid);
      const ws = memWorkspaces.find((w) => w.id === wsid) || memWorkspaces[0];
      const mx = mem.filter((x) => x.workspaceId === wsid).reduce((mm, x) => Math.max(mm, x.sortOrder || 0), 0);
      mem.push({
        id: rnd(10), name, description: String(e.description || ''), content: String(e.content || ''),
        category: String(e.category || ''), tags: String(e.tags || ''),
        scope: pid ? 'project' : 'global', projectId: pid,
        projectSlug: proj?.slug || '', projectName: proj?.name || '',
        workspaceId: wsid, workspaceSlug: ws?.slug || '', workspaceName: ws?.name || '',
        enabled: e.enabled !== false, sortOrder: mx + 1,
        createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
      });
      imported++;
    }
    const proj = memProjects.find((p) => p.id === pid);
    return { imported, skipped, scope: pid ? 'project' : 'global', project: proj?.slug };
  }),
  ProjectSkillCount: binding('ProjectSkillCount', async (id: string): Promise<number> => mem.filter((s) => s.projectId === id).length),
  ListWorkspaces: binding('ListWorkspaces', async (): Promise<Workspace[]> => [...memWorkspaces]),
  GetMainWorkspace: binding('GetMainWorkspace', async (): Promise<Workspace> => memWorkspaces.find((w) => w.isMain) || memWorkspaces[0]),
  CreateWorkspace: binding('CreateWorkspace', async (input: WorkspaceInput): Promise<Workspace> => {
    const slug = normSlug(input.slug || input.name);
    if (memWorkspaces.some((w) => w.slug === slug)) throw new Error(`workspace slug "${slug}" already exists`);
    const { gitToken, ...rest } = input;
    const w: Workspace = { ...rest, slug, id: rnd(10), gitBranch: input.gitBranch || 'main', hasToken: !!gitToken, isMain: false, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
    memWorkspaces.push(w);
    return w;
  }),
  UpdateWorkspace: binding('UpdateWorkspace', async (id: string, input: WorkspaceInput): Promise<Workspace> => {
    const i = memWorkspaces.findIndex((w) => w.id === id);
    if (i < 0) throw new Error('workspace not found');
    const { gitToken, ...rest } = input;
    memWorkspaces[i] = { ...memWorkspaces[i], ...rest, slug: normSlug(input.slug || input.name), updatedAt: new Date().toISOString() };
    if (gitToken) memWorkspaces[i].hasToken = true;
    return memWorkspaces[i];
  }),
  DeleteWorkspace: binding('DeleteWorkspace', async (id: string): Promise<void> => {
    if (memWorkspaces.find((w) => w.id === id)?.isMain) throw new Error('the main workspace cannot be deleted');
    memWorkspaces = memWorkspaces.filter((w) => w.id !== id);
    const pids = new Set(memProjects.filter((p) => p.workspaceId === id).map((p) => p.id));
    memProjects = memProjects.filter((p) => p.workspaceId !== id);
    mem = mem.filter((s) => s.workspaceId !== id && !pids.has(s.projectId));
  }),
  SetWorkspaceGit: binding('SetWorkspaceGit', async (id: string, remote: string, branch: string, token?: string): Promise<Workspace> => {
    const w = memWorkspaces.find((x) => x.id === id);
    if (!w) throw new Error('workspace not found');
    if (token) w.hasToken = true;
    else if (remote.trim() !== w.gitRemote) w.hasToken = false;
    if (!remote.trim()) w.hasToken = false;
    w.gitRemote = remote; w.gitBranch = branch || 'main'; w.updatedAt = new Date().toISOString();
    return w;
  }),
  WorkspaceGitStatus: binding('WorkspaceGitStatus', async (): Promise<string> => '(web-dev mock) local only'),
  PushWorkspace: binding('PushWorkspace', async (): Promise<any> => ({ ok: true, detail: '(web-dev mock) pushed' })),
  PullWorkspace: binding('PullWorkspace', async (): Promise<any> => ({ ok: true, imported: 0, skipped: [], detail: '(web-dev mock) pulled' })),
  CloneWorkspace: binding('CloneWorkspace', async (name: string, _slug?: string, _remote?: string, _branch?: string, _token?: string): Promise<Workspace> => {
    const slug = normSlug(name);
    const w: Workspace = { name, slug, description: '', color: '#6366f1', gitRemote: _remote || '', gitBranch: _branch || 'main', hasToken: !!_token, id: rnd(10), isMain: false, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
    memWorkspaces.push(w);
    return w;
  }),
  WorkspaceSkillCount: binding('WorkspaceSkillCount', async (id: string): Promise<number> => mem.filter((s) => s.workspaceId === id).length),
  MoveSkillTo: binding('MoveSkillTo', async (id: string, scope: string, projectId: string): Promise<Skill> => {
    const s = mem.find((x) => x.id === id);
    if (!s) throw new Error('skill not found');
    if (scope === 'project') {
      const p = memProjects.find((x) => x.id === projectId);
      if (!p) throw new Error('unknown project');
      s.scope = 'project'; s.projectId = p.id; s.projectSlug = p.slug; s.projectName = p.name;
      s.workspaceId = p.workspaceId; s.workspaceSlug = p.workspaceSlug; s.workspaceName = p.workspaceName;
    } else {
      s.scope = 'global'; s.projectId = ''; s.projectSlug = ''; s.projectName = '';
    }
    s.updatedAt = new Date().toISOString();
    return s;
  }),
  GetSettings: binding('GetSettings', async (): Promise<Record<string, string>> => ({})),
  SetSetting: binding('SetSetting', async (): Promise<void> => {}),
  GetLogs: binding('GetLogs', async (): Promise<string[]> => ['(web-dev mock) skillsmcp started']),
  LogPath: binding('LogPath', async (): Promise<string> => '~/.skillsmcp/skillsmcp.log'),
  ClearLogs: binding('ClearLogs', async (): Promise<void> => {}),
  MCPStatus: binding('MCPStatus', async (): Promise<any> => ({ running: false, url: 'http://127.0.0.1:9423' })),
  StartMCP: binding('StartMCP', async (): Promise<string> => 'http://127.0.0.1:9423'),
  MCPToolsPreview: binding('MCPToolsPreview', async (): Promise<string> =>
    'list_skills · get_skill(name) · list_projects · ' + mem.filter((s) => s.enabled && s.scope === 'global').map((s) => s.name).join(' · ')),
  ControlMCPToolsPreview: binding('ControlMCPToolsPreview', async (): Promise<string> =>
    'app_help · list_skills · get_skill · list_projects · list_project_skills · create_skill · update_skill · delete_skill · set_skill_enabled · create_project · update_project · delete_project · export_skills · import_skills · list_workspaces · create_workspace · update_workspace · delete_workspace · set_workspace_git · push_workspace · pull_workspace · workspace_status · clone_workspace'),
  ProjectMCPToolsPreview: binding('ProjectMCPToolsPreview', async (slug: string): Promise<string> =>
    'list_skills · get_skill(name) · ' + mem.filter((s) => s.enabled && (s.scope === 'global' || s.projectSlug === slug)).map((s) => s.name).join(' · ')),
  WorkspaceMCPToolsPreview: binding('WorkspaceMCPToolsPreview', async (workspaceId: string): Promise<string> =>
    'list_skills · get_skill(name) · list_projects · ' + mem.filter((s) => s.enabled && s.scope === 'global' && s.workspaceId === workspaceId).map((s) => s.name).join(' · ')),
  ProjectMCPToolsPreviewIn: binding('ProjectMCPToolsPreviewIn', async (workspaceId: string, slug: string): Promise<string> =>
    'list_skills · get_skill(name) · ' + mem.filter((s) => s.enabled && s.workspaceId === workspaceId && (s.scope === 'global' || s.projectSlug === slug)).map((s) => s.name).join(' · ')),
  OpencodeConfig: binding('OpencodeConfig', async (): Promise<string> => JSON.stringify({
    $schema: 'https://opencode.ai/config.json',
    mcp: { skillsmcp: { type: 'local', command: ['SkillsMCP.exe', 'mcp'], enabled: true } },
  }, null, 2)),
  OpencodeConfigForProject: binding('OpencodeConfigForProject', async (slug: string): Promise<string> => JSON.stringify({
    $schema: 'https://opencode.ai/config.json',
    mcp: { [`skillsmcp-${slug}`]: { type: 'local', command: ['SkillsMCP.exe', 'mcp', '--project', slug], enabled: true } },
  }, null, 2)),
  OpencodeConfigForWorkspace: binding('OpencodeConfigForWorkspace', async (workspaceId: string): Promise<string> => {
    const w = memWorkspaces.find((x) => x.id === workspaceId) || memWorkspaces[0];
    const key = w.isMain ? 'skillsmcp' : `skillsmcp-${w.slug}`;
    const cmd = w.isMain ? ['SkillsMCP.exe', 'mcp'] : ['SkillsMCP.exe', 'mcp', '--workspace', w.slug];
    return JSON.stringify({ $schema: 'https://opencode.ai/config.json', mcp: { [key]: { type: 'local', command: cmd, enabled: true } } }, null, 2);
  }),
  OpencodeConfigForProjectIn: binding('OpencodeConfigForProjectIn', async (workspaceId: string, slug: string): Promise<string> => {
    const w = memWorkspaces.find((x) => x.id === workspaceId) || memWorkspaces[0];
    const key = w.isMain ? `skillsmcp-${slug}` : `skillsmcp-${w.slug}-${slug}`;
    const cmd = w.isMain ? ['SkillsMCP.exe', 'mcp', '--project', slug] : ['SkillsMCP.exe', 'mcp', '--workspace', w.slug, '--project', slug];
    return JSON.stringify({ $schema: 'https://opencode.ai/config.json', mcp: { [key]: { type: 'local', command: cmd, enabled: true } } }, null, 2);
  }),
  OpencodeConfigControl: binding('OpencodeConfigControl', async (): Promise<string> => JSON.stringify({
    $schema: 'https://opencode.ai/config.json',
    mcp: { 'skillsmcp-control': { type: 'local', command: ['SkillsMCP.exe', 'mcp', '--control'], enabled: true } },
  }, null, 2)),
  ClaudeConfig: binding('ClaudeConfig', async (): Promise<string> => JSON.stringify({
    mcpServers: { skillsmcp: { command: 'SkillsMCP.exe', args: ['mcp'] } },
  }, null, 2)),
  ClaudeConfigForProject: binding('ClaudeConfigForProject', async (slug: string): Promise<string> => JSON.stringify({
    mcpServers: { [`skillsmcp-${slug}`]: { command: 'SkillsMCP.exe', args: ['mcp', '--project', slug] } },
  }, null, 2)),
  ClaudeConfigForWorkspace: binding('ClaudeConfigForWorkspace', async (workspaceId: string): Promise<string> => {
    const w = memWorkspaces.find((x) => x.id === workspaceId) || memWorkspaces[0];
    const key = w.isMain ? 'skillsmcp' : `skillsmcp-${w.slug}`;
    const args = w.isMain ? ['mcp'] : ['mcp', '--workspace', w.slug];
    return JSON.stringify({ mcpServers: { [key]: { command: 'SkillsMCP.exe', args } } }, null, 2);
  }),
  ClaudeConfigForProjectIn: binding('ClaudeConfigForProjectIn', async (workspaceId: string, slug: string): Promise<string> => {
    const w = memWorkspaces.find((x) => x.id === workspaceId) || memWorkspaces[0];
    const key = w.isMain ? `skillsmcp-${slug}` : `skillsmcp-${w.slug}-${slug}`;
    const args = w.isMain ? ['mcp', '--project', slug] : ['mcp', '--workspace', w.slug, '--project', slug];
    return JSON.stringify({ mcpServers: { [key]: { command: 'SkillsMCP.exe', args } } }, null, 2);
  }),
  ClaudeConfigControl: binding('ClaudeConfigControl', async (): Promise<string> => JSON.stringify({
    mcpServers: { 'skillsmcp-control': { command: 'SkillsMCP.exe', args: ['mcp', '--control'] } },
  }, null, 2)),
  TestMCP: binding('TestMCP', async (): Promise<string> =>
    'stdio msg 1 → {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",...}}\ntools/list → 200 (web-dev mock)'),
  TestProjectMCP: binding('TestProjectMCP', async (slug: string): Promise<string> => `(web-dev mock) project MCP skillsmcp-${slug} OK`),
  TestWorkspaceMCP: binding('TestWorkspaceMCP', async (workspaceId: string): Promise<string> => `(web-dev mock) workspace MCP ${workspaceId} OK`),
  TestProjectMCPIn: binding('TestProjectMCPIn', async (workspaceId: string, slug: string): Promise<string> => `(web-dev mock) project MCP ${workspaceId}/${slug} OK`),
  TestControlMCP: binding('TestControlMCP', async (): Promise<string> => '(web-dev mock) control MCP skillsmcp-control OK'),
  CheckForUpdates: binding('CheckForUpdates', async (): Promise<UpdateInfo> => ({
    currentVersion: 'dev', latestVersion: 'dev', releaseName: '', notes: '',
    pageUrl: '', assetName: '', downloadUrl: '', size: 0, publishedAt: '',
    updateAvailable: false, canInstall: false, platform: 'web',
  })),
  SkipUpdateVersion: binding('SkipUpdateVersion', async (): Promise<void> => {}),
  OpenReleasePage: binding('OpenReleasePage', async (): Promise<string> => ''),
  DownloadAndInstallUpdate: binding('DownloadAndInstallUpdate', async (): Promise<string> => 'web-dev mock: no updater here'),
};
