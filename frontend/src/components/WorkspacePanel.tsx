import { useEffect, useState } from 'react';
import {
  Briefcase, Copy, Check, Pencil, Trash2, Github, ArrowUpToLine, ArrowDownToLine,
  RefreshCw, FlaskConical, Loader2, Plus, FolderKanban, Globe,
} from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill, Workspace } from '../lib/types';
import { Badge, Button, Card, Input } from './ui';
import { cn } from '../lib/cn';

// Workspace tab: edit the workspace, link a git repo + push/pull,
// see per-workspace MCP commands, connect a repo as a new workspace.
export default function WorkspacePanel({ workspace, projects, skills, onChanged, onError }: {
  workspace: Workspace;
  projects: Project[];
  skills: Skill[];
  onChanged: (switchToId?: string) => void;
  onError: (msg: string) => void;
}) {
  const [edit, setEdit] = useState({ name: workspace.name, slug: workspace.slug, description: workspace.description, color: workspace.color });
  const [saving, setSaving] = useState(false);
  const [git, setGit] = useState({ remote: workspace.gitRemote || '', branch: workspace.gitBranch || 'main', token: '' });
  const [gitSaving, setGitSaving] = useState(false);
  const [gitBusy, setGitBusy] = useState<'push' | 'pull' | null>(null);
  const [gitOut, setGitOut] = useState('');
  const [status, setStatus] = useState('loading…');
  const [clone, setClone] = useState({ name: '', remote: '', branch: 'main', token: '' });
  const [cloneBusy, setCloneBusy] = useState(false);
  const [copied, setCopied] = useState('');
  const [projOut, setProjOut] = useState<Record<string, string>>({});
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    setEdit({ name: workspace.name, slug: workspace.slug, description: workspace.description, color: workspace.color });
    setGit((g) => ({ remote: workspace.gitRemote || '', branch: workspace.gitBranch || 'main', token: '' }));
    setGitOut('');
    setConfirmDelete(false);
    reloadStatus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace.id]);

  async function reloadStatus() {
    try {
      setStatus(String(await (api as any).WorkspaceGitStatus(workspace.id)));
    } catch {
      setStatus('status unavailable');
    }
  }

  async function copy(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      setTimeout(() => setCopied(''), 1400);
    } catch (e: any) {
      onError(e?.message || String(e));
    }
  }

  function wsCommand(): string {
    return workspace.isMain ? 'SkillsMCP mcp' : `SkillsMCP mcp --workspace ${workspace.slug}`;
  }

  function projCommand(p: Project): string {
    return workspace.isMain ? `SkillsMCP mcp --project ${p.slug}` : `SkillsMCP mcp --workspace ${workspace.slug} --project ${p.slug}`;
  }

  async function saveEdit() {
    if (!edit.name.trim()) return;
    setSaving(true);
    try {
      await api.UpdateWorkspace(workspace.id, { ...edit, name: edit.name.trim(), slug: edit.slug.trim(), gitRemote: workspace.gitRemote, gitBranch: workspace.gitBranch });
      onChanged();
    } catch (e: any) {
      onError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  async function saveGit() {
    setGitSaving(true);
    try {
      await api.SetWorkspaceGit(workspace.id, git.remote.trim(), git.branch.trim() || 'main', git.token.trim());
      setGit((g) => ({ ...g, token: '' }));
      onChanged();
      reloadStatus();
    } catch (e: any) {
      onError(e?.message || String(e));
    } finally {
      setGitSaving(false);
    }
  }

  async function doPush() {
    setGitBusy('push');
    setGitOut('');
    try {
      const r = (await (api as any).PushWorkspace(workspace.id)) as any;
      setGitOut(r?.ok ? `✓ ${r.detail || 'pushed'}` : `failed: ${r?.error || r?.detail || 'unknown'}`);
      onChanged();
      reloadStatus();
    } catch (e: any) {
      setGitOut('failed: ' + (e?.message || String(e)));
    } finally {
      setGitBusy(null);
    }
  }

  async function doPull() {
    setGitBusy('pull');
    setGitOut('');
    try {
      const r = (await (api as any).PullWorkspace(workspace.id)) as any;
      setGitOut(r?.ok ? `✓ ${r.detail || 'pulled'}` : `failed: ${r?.error || 'unknown'}`);
      onChanged();
      reloadStatus();
    } catch (e: any) {
      setGitOut('failed: ' + (e?.message || String(e)));
    } finally {
      setGitBusy(null);
    }
  }

  async function doClone() {
    if (!clone.name.trim() || !clone.remote.trim()) return;
    setCloneBusy(true);
    try {
      const ws = (await (api as any).CloneWorkspace(clone.name.trim(), '', clone.remote.trim(), clone.branch.trim() || 'main', clone.token.trim())) as Workspace;
      setClone({ name: '', remote: '', branch: 'main', token: '' });
      onChanged(ws.id);
    } catch (e: any) {
      onError(e?.message || String(e));
    } finally {
      setCloneBusy(false);
    }
  }

  async function doDelete() {
    try {
      await api.DeleteWorkspace(workspace.id);
      onChanged('');
    } catch (e: any) {
      onError(e?.message || String(e));
    }
  }

  async function copyOpencodeWs() {
    copy(String(await (api as any).OpencodeConfigForWorkspace(workspace.id)), 'ws-json');
  }

  async function testWs() {
    try {
      setProjOut((t) => ({ ...t, __ws: 'testing…' }));
      const out = String(await (api as any).TestWorkspaceMCP(workspace.id));
      setProjOut((t) => ({ ...t, __ws: out }));
    } catch (e: any) {
      setProjOut((t) => ({ ...t, __ws: 'failed: ' + (e?.message || String(e)) }));
    }
  }

  const globals = skills.filter((s) => s.scope !== 'project');

  return (
    <div className="mx-auto grid max-w-6xl gap-3">
      {/* header */}
      <Card className="flex flex-wrap items-center gap-3 p-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg text-white" style={{ background: workspace.color || '#52525b' }}>
          <Briefcase size={18} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[15px] font-semibold text-white">{workspace.name}</span>
            {workspace.isMain
              ? <Badge tone="green">main · personal</Badge>
              : <Badge tone="indigo">skillsmcp-{workspace.slug}</Badge>}
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
            {globals.filter((s) => s.enabled).length}/{globals.length} global · {projects.length} project(s) · <code className="text-zinc-300">{wsCommand()}</code>
          </div>
        </div>
        <div className="flex gap-1.5">
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={() => copy(wsCommand(), 'ws-cmd')}>
            {copied === 'ws-cmd' ? <Check size={12} /> : <Copy size={12} />} Command
          </Button>
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={copyOpencodeWs}>
            {copied === 'ws-json' ? <Check size={12} /> : <Copy size={12} />} opencode JSON
          </Button>
          <Button variant="ghost" className="!py-1.5 text-[11px]" onClick={testWs}>
            <FlaskConical size={12} /> Test
          </Button>
        </div>
      </Card>
      {projOut.__ws && (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2.5 font-mono text-[10px] text-zinc-300">{projOut.__ws}</pre>
      )}

      {/* git sync */}
      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <Github size={14} className="text-zinc-400" /> Git sync — push to / pull from GitHub or any repo
        </div>
        <div className="mb-2.5 text-[11px] leading-relaxed text-zinc-500">
          Manual sync, nothing pushes itself. SSH URLs (<code className="font-mono">git@github.com:org/repo.git</code>) use your keys/agent —
          no token needed. For <span className="text-zinc-300">private HTTPS repos</span> paste a token (e.g. GitHub PAT): it is stored
          locally, never shown again, never written into the repo, and sent per-command as a header.
          Leave blank to keep the saved one; changing remote without a token drops the old one.
          Layout on disk: <code className="font-mono">workspace.json</code> + <code className="font-mono">globals/</code> + <code className="font-mono">projects/&lt;slug&gt;/</code> (same lossless format as Export).
        </div>
        <div className="grid gap-2 md:grid-cols-[1fr_160px]">
          <Input value={git.remote} onChange={(e) => setGit((g) => ({ ...g, remote: e.target.value }))} placeholder="git@github.com:org/skills.git  (empty = local only)" spellCheck={false} className="font-mono !text-[12px]" />
          <Input value={git.branch} onChange={(e) => setGit((g) => ({ ...g, branch: e.target.value }))} placeholder="main" spellCheck={false} className="font-mono !text-[12px]" />
        </div>
        <div className="mt-2 grid gap-2 md:grid-cols-[1fr_auto]">
          <div className="relative">
            <Input value={git.token} onChange={(e) => setGit((g) => ({ ...g, token: e.target.value }))} placeholder={workspace.hasToken ? '•••••• token saved — leave blank to keep' : 'Token for private HTTPS repos (optional)'} spellCheck={false} type="password" className="font-mono !text-[12px]" />
          </div>
          <div className="flex items-center gap-1.5">
            {workspace.hasToken && <Badge tone="green">token saved</Badge>}
          </div>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={saveGit} disabled={gitSaving}>
            {gitSaving ? <Loader2 size={12} className="animate-spin" /> : <Check size={12} />} Link repo
          </Button>
          <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={doPush} disabled={gitBusy !== null || !workspace.gitRemote}>
            {gitBusy === 'push' ? <Loader2 size={12} className="animate-spin" /> : <ArrowUpToLine size={12} />} Push
          </Button>
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={doPull} disabled={gitBusy !== null || !workspace.gitRemote}>
            {gitBusy === 'pull' ? <Loader2 size={12} className="animate-spin" /> : <ArrowDownToLine size={12} />} Pull
          </Button>
          <button onClick={reloadStatus} className="ml-auto flex items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-200">
            <RefreshCw size={11} /> status
          </button>
        </div>
        <div className="mt-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-2 font-mono text-[11px] text-zinc-400">{status}</div>
        {gitOut && <div className="mt-1.5 whitespace-pre-wrap font-mono text-[11px] text-zinc-300">{gitOut}</div>}
      </Card>

      {/* projects + their MCPs */}
      <Card className="p-4">
        <div className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <FolderKanban size={14} className="text-indigo-300" /> Projects — each keeps its own MCP
        </div>
        {projects.length === 0 ? (
          <div className="rounded-lg border border-dashed border-zinc-800 px-3 py-3 text-center text-[11px] text-zinc-600">
            No projects in {workspace.name} yet — create one in the Projects tab.
          </div>
        ) : (
          <div className="grid gap-2">
            {projects.map((p) => {
              const live = skills.filter((s) => s.projectId === p.id && s.enabled).length;
              const total = skills.filter((s) => s.projectId === p.id).length;
              return (
                <div key={p.id} className="flex flex-wrap items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-2">
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: p.color }} />
                  <span className="text-[12px] font-medium text-zinc-100">{p.name}</span>
                  <code className="truncate font-mono text-[10px] text-emerald-300">{projCommand(p)}</code>
                  <span className="font-mono text-[10px] text-zinc-600">{live}/{total} live</span>
                  <span className="ml-auto flex gap-1">
                    <Button variant="ghost" className="!px-2 !py-1 text-[11px]" onClick={() => copy(projCommand(p), `cmd-${p.id}`)}>
                      {copied === `cmd-${p.id}` ? <Check size={11} /> : <Copy size={11} />}
                    </Button>
                    <Button variant="ghost" className="!px-2 !py-1 text-[11px]" onClick={async () => {
                      setProjOut((t) => ({ ...t, [p.id]: 'testing…' }));
                      try {
                        const out = String(await (api as any).TestProjectMCPIn(workspace.id, p.slug));
                        setProjOut((t) => ({ ...t, [p.id]: out }));
                      } catch (e: any) {
                        setProjOut((t) => ({ ...t, [p.id]: 'failed: ' + (e?.message || String(e)) }));
                      }
                    }}>
                      <FlaskConical size={11} />
                    </Button>
                  </span>
                  {projOut[p.id] && (
                    <pre className="max-h-32 w-full overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2 font-mono text-[10px] text-zinc-300">{projOut[p.id]}</pre>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </Card>

      {/* connect a repo as a new workspace */}
      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <Plus size={14} className="text-emerald-300" /> Connect a repo as a new workspace
        </div>
        <div className="mb-2 text-[11px] text-zinc-500">
          Clones a public or private repo and imports every skill (SSH/agent or token below). Each project inside keeps its own MCP.
        </div>
        <div className="grid gap-2 md:grid-cols-[200px_1fr_140px]">
          <Input value={clone.name} onChange={(e) => setClone((c) => ({ ...c, name: e.target.value }))} placeholder="Workspace name" />
          <Input value={clone.remote} onChange={(e) => setClone((c) => ({ ...c, remote: e.target.value }))} placeholder="git@github.com:org/skills.git" spellCheck={false} className="font-mono !text-[12px]" />
          <Input value={clone.branch} onChange={(e) => setClone((c) => ({ ...c, branch: e.target.value }))} placeholder="main" spellCheck={false} className="font-mono !text-[12px]" />
        </div>
        <div className="mt-2">
          <Input value={clone.token} onChange={(e) => setClone((c) => ({ ...c, token: e.target.value }))} placeholder="Token for private HTTPS repos (optional, stored, never shown)" spellCheck={false} type="password" className="font-mono !text-[12px]" />
        </div>
        <div className="mt-2">
          <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={doClone} disabled={cloneBusy || !clone.name.trim() || !clone.remote.trim()}>
            {cloneBusy ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />} Clone & import
          </Button>
        </div>
      </Card>

      {/* edit + danger */}
      <Card className="p-4">
        <div className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <Pencil size={13} className="text-zinc-400" /> Edit workspace
        </div>
        <div className="grid gap-2 md:grid-cols-2">
          <Input value={edit.name} onChange={(e) => setEdit((f) => ({ ...f, name: e.target.value }))} placeholder="Name" />
          <Input value={edit.slug} onChange={(e) => setEdit((f) => ({ ...f, slug: e.target.value.toLowerCase() }))} placeholder="slug" spellCheck={false} className="font-mono" />
        </div>
        <div className="mt-2">
          <Input value={edit.description} onChange={(e) => setEdit((f) => ({ ...f, description: e.target.value }))} placeholder="Description" />
        </div>
        <div className="mt-2 flex items-center gap-2">
          <Input value={edit.color} onChange={(e) => setEdit((f) => ({ ...f, color: e.target.value }))} placeholder="#6366f1" spellCheck={false} className="font-mono !w-32" />
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={saveEdit} disabled={saving}>
            {saving ? <Loader2 size={12} className="animate-spin" /> : <Check size={12} />} Save
          </Button>
          {!workspace.isMain && (
            <span className="ml-auto">
              {!confirmDelete ? (
                <Button variant="ghost" className="!py-1.5 text-[11px] hover:!text-red-300" onClick={() => setConfirmDelete(true)}>
                  <Trash2 size={12} /> Delete…
                </Button>
              ) : (
                <span className="flex items-center gap-1.5 text-[11px] text-red-300">
                  Delete {workspace.slug} + everything in it?
                  <Button variant="outline" className="!py-1 text-[11px] hover:!text-red-300" onClick={doDelete}>Confirm</Button>
                  <Button variant="ghost" className="!py-1 text-[11px]" onClick={() => setConfirmDelete(false)}>Cancel</Button>
                </span>
              )}
            </span>
          )}
        </div>
        {workspace.isMain && (
          <div className="mt-1.5 flex items-center gap-1.5 text-[11px] text-zinc-600">
            <Globe size={11} /> The main workspace is personal and cannot be deleted.
          </div>
        )}
      </Card>
    </div>
  );
}
