import { useEffect, useState } from 'react';
import {
  Briefcase, Copy, Check, Pencil, Trash2,
  FlaskConical, Loader2, FolderKanban, Globe,
} from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill, Workspace } from '../lib/types';
import { Badge, Button, Card, Input } from './ui';

// Workspace tab: edit the workspace and see per-workspace MCP commands.
// Git sync lives in the dedicated Sync tab (per-workspace remote, push/pull,
// conflicts, clone) — this tab stays focused on identity + projects.
export default function WorkspacePanel({ workspace, projects, skills, onChanged, onError }: {
  workspace: Workspace;
  projects: Project[];
  skills: Skill[];
  onChanged: (switchToId?: string) => void;
  onError: (msg: string) => void;
}) {
  const [edit, setEdit] = useState({ name: workspace.name, slug: workspace.slug, description: workspace.description, color: workspace.color });
  const [saving, setSaving] = useState(false);
  const [copied, setCopied] = useState('');
  const [projOut, setProjOut] = useState<Record<string, string>>({});
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    setEdit({ name: workspace.name, slug: workspace.slug, description: workspace.description, color: workspace.color });
    setConfirmDelete(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace.id]);

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
              ? <Badge tone="green">personal · default</Badge>
              : <Badge tone="indigo">skillsmcp-{workspace.slug}</Badge>}
            {workspace.gitRemote
              ? <Badge tone="green">sync linked — see Sync tab</Badge>
              : <Badge tone="zinc">local only — link a repo in Sync</Badge>}
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
            <Globe size={11} /> The Personal workspace is the default and cannot be deleted.
          </div>
        )}
      </Card>
    </div>
  );
}
