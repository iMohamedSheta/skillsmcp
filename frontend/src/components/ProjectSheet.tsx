import { useEffect, useState } from 'react';
import { Check, Loader2 } from 'lucide-react';
import { api } from '../lib/api';
import type { Project, ProjectInput } from '../lib/types';
import { BottomSheet, Button, Input } from './ui';

export const EMPTY_PROJECT: ProjectInput = { name: '', slug: '', description: '', color: '', workspaceId: '' };

const PALETTE = ['#10b981', '#6366f1', '#f59e0b', '#ec4899', '#06b6d4', '#8b5cf6', '#f43f5e', '#14b8a6'];

export default function ProjectSheet({ open, onClose, initial, editing, workspaceId, workspaceSlug, onSaved }: {
  open: boolean; onClose: () => void;
  initial: ProjectInput; editing: Project | null;
  workspaceId: string; workspaceSlug: string;
  onSaved: (p: Project) => void;
}) {
  const [form, setForm] = useState<ProjectInput>({ ...EMPTY_PROJECT });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (open) {
      setForm({ ...initial });
      setErr('');
    }
  }, [open ]);

  function patch<K extends keyof ProjectInput>(k: K, v: ProjectInput[K]) {
    setForm((f) => ({ ...f, [k]: v }));
  }

  async function normalizeSlug() {
    try {
      const n = String(await api.NormalizeSlug(form.slug || form.name));
      if (n && n !== form.slug) patch('slug', n);
    } catch {}
  }

  async function save() {
    setSaving(true);
    setErr('');
    try {
      const payload: ProjectInput = {
        name: form.name.trim(),
        slug: (form.slug || form.name).toLowerCase().trim(),
        description: form.description.trim(),
        color: form.color,
        workspaceId: editing ? (editing.workspaceId || workspaceId) : (form.workspaceId || workspaceId),
      };
      const saved = editing
        ? ((await api.UpdateProject(editing.id, payload)) as Project)
        : ((await api.CreateProject(payload)) as Project);
      onSaved(saved);
      onClose();
    } catch (e: any) {
      setErr(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <BottomSheet open={open} onClose={onClose}
      title={editing ? `Edit project · ${editing.slug}` : 'New project → own MCP'}
      subtitle="Gets its own MCP: SkillsMCP mcp --project <slug> (globals + project skills)">
      <div className="grid gap-3 p-4">
        <div className="grid gap-3 md:grid-cols-2">
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Name</div>
            <Input value={form.name} onChange={(e) => patch('name', e.target.value)} placeholder="e.g. My App" />
          </div>
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Slug · MCP name</div>
            <Input value={form.slug} onChange={(e) => patch('slug', e.target.value.toLowerCase())} onBlur={normalizeSlug}
              placeholder="e.g. my-app" spellCheck={false} className="font-mono" />
            <div className="mt-1 font-mono text-[10px] text-zinc-600">MCP: skillsmcp-{form.slug || '…'} · mcp --project {form.slug || '…'}</div>
          </div>
        </div>
        <div>
          <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Description</div>
          <Input value={form.description} onChange={(e) => patch('description', e.target.value)} placeholder="What is this project about?" />
        </div>
        <div>
          <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Color</div>
          <div className="flex flex-wrap gap-1.5">
            {PALETTE.map((c) => (
              <button key={c} onClick={() => patch('color', c)} title={c}
                className={`h-8 w-8 rounded-lg border-2 transition active:scale-95 ${form.color === c ? 'border-white/80' : 'border-transparent hover:scale-105'}`}
                style={{ background: c }} />
            ))}
          </div>
        </div>
        {err && <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-300">{err}</div>}
        <div className="sticky bottom-0 flex gap-2 border-t border-zinc-800 bg-zinc-950/95 py-3 backdrop-blur">
          <Button variant="ghost" onClick={onClose} className="flex-1">Cancel</Button>
          <Button variant="emerald" onClick={save} disabled={saving || !form.name.trim()} className="flex-[2]">
            {saving ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
            {editing ? 'Save project' : 'Create project'}
          </Button>
        </div>
      </div>
    </BottomSheet>
  );
}
