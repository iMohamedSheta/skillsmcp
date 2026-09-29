import { useEffect, useRef, useState } from 'react';
import { Check, Globe, FolderKanban, Loader2, Power, RotateCcw, History } from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill, SkillInput } from '../lib/types';
import { clearDraft, draftKeyFor, loadDraft, saveDraft } from '../lib/drafts';
import { BottomSheet, ConfirmModal } from './ui';
import { Button, Input, Tip } from './ui';
import MarkdownEditor from './MarkdownEditor';
import { cn } from '../lib/cn';

export const EMPTY_SKILL: SkillInput = {
  name: '', description: '', content: '', category: '', tags: '',
  scope: 'global', projectId: '', workspaceId: '', enabled: true,
};

export function skillToInput(s: Skill): SkillInput {
  return {
    name: s.name, description: s.description, content: s.content,
    category: s.category || '', tags: s.tags || '',
    scope: s.scope === 'project' ? 'project' : 'global',
    projectId: s.projectId || '', workspaceId: s.workspaceId || '', enabled: s.enabled,
  };
}

// Bottom-sheet skill editor with draft memory: closing the sheet never
// loses text — every keystroke persists to localStorage and is restored
// on reopen. Only an explicit confirmed Reset clears it (or a save).
export default function SkillSheet({ open, onClose, initial, editing, projects, workspaceId, workspaceName, onSaved }: {
  open: boolean; onClose: () => void;
  initial: SkillInput; editing: Skill | null;
  projects: Project[];
  workspaceId: string; workspaceName: string;
  onSaved: (s: Skill) => void;
}) {
  const key = draftKeyFor(editing ? editing.id : null);
  const [form, setForm] = useState<SkillInput>({ ...EMPTY_SKILL });
  const [restored, setRestored] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');
  const [askReset, setAskReset] = useState(false);
  const keyRef = useRef(key);
  keyRef.current = key;

  // (Re)load when the sheet opens or targets another skill.
  // A stored draft ALWAYS wins over the fresh initial — that is the point:
  // closing the sheet must not remove text.
  useEffect(() => {
    if (!open) return;
    const d = loadDraft(keyRef.current);
    if (d) {
      setForm(d);
      setRestored(true);
    } else {
      setForm({ ...initial });
      setRestored(false);
    }
    setErr('');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing?.id]);

  function patch<K extends keyof SkillInput>(k: K, v: SkillInput[K]) {
    setForm((f) => {
      const next = { ...f, [k]: v };
      saveDraft(keyRef.current, next);
      return next;
    });
  }

  function freshForm(): SkillInput {
    // Reset = back to blank (new) or to the saved skill (edit).
    if (editing) return skillToInput(editing);
    return { ...EMPTY_SKILL, scope: initial.scope, projectId: initial.projectId, workspaceId: initial.workspaceId || workspaceId };
  }

  function doReset() {
    clearDraft(keyRef.current);
    setForm(freshForm());
    setRestored(false);
    setErr('');
    setAskReset(false);
  }

  async function normalizeName() {
    try {
      const n = String(await api.NormalizeName(form.name));
      if (n && n !== form.name) patch('name', n);
    } catch {}
  }

  async function save() {
    setSaving(true);
    setErr('');
    try {
      const payload: SkillInput = {
        name: form.name.toLowerCase().trim(),
        description: form.description.trim(),
        content: form.content,
        category: form.category.trim(),
        tags: form.tags.trim(),
        scope: form.scope === 'project' ? 'project' : 'global',
        projectId: form.scope === 'project' ? form.projectId : '',
        workspaceId: editing ? (editing.workspaceId || workspaceId) : (form.workspaceId || workspaceId),
        enabled: form.enabled,
      };
      const saved = editing
        ? ((await api.UpdateSkill(editing.id, payload)) as Skill)
        : ((await api.CreateSkill(payload)) as Skill);
      clearDraft(keyRef.current);
      setRestored(false);
      onSaved(saved);
      onClose();
    } catch (e: any) {
      setErr(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  const canSave = form.name.trim() !== '' && form.description.trim() !== '' && form.content.trim() !== ''
    && (form.scope !== 'project' || form.projectId !== '');

  return (
    <>
      <BottomSheet open={open} onClose={onClose} wide
        title={editing ? `Edit skill · ${editing.name}` : 'New skill → new MCP tool'}
        subtitle={editing
          ? 'Changes go live on the next tools/list — no restart'
          : `Global = workspace MCP · Project = that project\u2019s own MCP · in ${workspaceName || 'workspace'}`}>
        <div className="grid gap-3 p-4">
          {restored && (
            <div className="flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-amber-200">
              <History size={12} className="shrink-0" />
              <span>Draft restored — closing the sheet never loses text.</span>
              <button onClick={() => setAskReset(true)} className="ml-auto shrink-0 underline hover:text-amber-100">Reset it</button>
            </div>
          )}

          {/* scope switch */}
          <div className="grid grid-cols-2 gap-1 rounded-lg border border-zinc-800 bg-zinc-900 p-1">
            {([
              { v: 'global', icon: Globe, label: 'Global', hint: 'main MCP' },
              { v: 'project', icon: FolderKanban, label: 'Project', hint: 'project MCP' },
            ] as const).map((o) => (
              <button key={o.v} onClick={() => patch('scope', o.v)}
                className={cn('flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-xs transition',
                  form.scope === o.v ? 'acc-bg acc-on font-semibold' : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100')}>
                <o.icon size={13} /> {o.label}
                <span className={cn('font-mono text-[10px]', form.scope === o.v ? 'opacity-70' : 'text-zinc-600')}>{o.hint}</span>
              </button>
            ))}
          </div>

          {form.scope === 'project' && (
            <div>
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Project · its own MCP serves this skill</div>
              {projects.length === 0 ? (
                <div className="rounded-lg border border-dashed border-zinc-700 p-3 text-center text-[11px] text-zinc-500">
                  No projects yet — create one in the Projects tab first.
                </div>
              ) : (
                <div className="grid max-h-40 gap-1 overflow-y-auto">
                  {projects.map((p) => (
                    <button key={p.id} onClick={() => patch('projectId', p.id)}
                      className={cn('flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-xs transition',
                        form.projectId === p.id
                          ? 'acc-border acc-soft border'
                          : 'border-zinc-800 hover:bg-zinc-800')}>
                      <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: p.color || '#52525b' }} />
                      <span className="font-medium text-zinc-100">{p.name}</span>
                      <span className="truncate font-mono text-[10px] text-zinc-500">skillsmcp-{p.slug}</span>
                      {form.projectId === p.id && <Check size={13} className="acc-text ml-auto shrink-0" />}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          <div className="grid gap-3 md:grid-cols-2">
            <div>
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Tool name · slug</div>
              <Input value={form.name} onChange={(e) => patch('name', e.target.value.toLowerCase())} onBlur={normalizeName}
                placeholder="e.g. git-commit" spellCheck={false} className="font-mono" />
              <div className="mt-1 font-mono text-[10px] text-zinc-600">MCP tool: {form.name || '…'} · reserved: list_skills, get_skill, list_projects</div>
            </div>
            <div>
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Category</div>
              <Input value={form.category} onChange={(e) => patch('category', e.target.value)} placeholder="e.g. git, db, api" spellCheck={false} className="font-mono" />
            </div>
          </div>

          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Description · the AI reads this to decide when to call it</div>
            <Input value={form.description} onChange={(e) => patch('description', e.target.value)}
              placeholder="e.g. How to write Conventional Commits messages. Call this before creating any git commit." />
          </div>

          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Tags (comma separated)</div>
            <Input value={form.tags} onChange={(e) => patch('tags', e.target.value)} placeholder="git, commit, conventional-commits" spellCheck={false} className="font-mono" />
          </div>

          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Content · markdown the AI receives</div>
            <MarkdownEditor value={form.content} onChange={(v) => patch('content', v)} heightClass="h-[38vh]" />
          </div>

          <div className="flex items-center gap-2">
            <button onClick={() => patch('enabled', !form.enabled)}
              className={cn('flex items-center gap-1.5 rounded-lg border px-2.5 py-2 text-[11px]',
                form.enabled ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-200' : 'border-zinc-700 text-zinc-400 hover:bg-zinc-800')}>
              <Power size={12} /> {form.enabled ? 'Enabled' : 'Disabled'}
            </button>
            <span className="text-[11px] text-zinc-600">
              {form.scope === 'project'
                ? 'Served by its project MCP (+ globals)'
                : 'Served by the main MCP (+ every project MCP)'}
            </span>
          </div>

          {err && <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-300">{err}</div>}

          <div className="sticky bottom-0 flex gap-2 border-t border-zinc-800 bg-zinc-950/95 py-3 backdrop-blur">
            <Tip label="Clear the remembered draft and start blank (asks first)">
              <Button variant="ghost" onClick={() => setAskReset(true)} className="!px-3">
                <RotateCcw size={14} /> Reset
              </Button>
            </Tip>
            <Button variant="ghost" onClick={onClose} className="flex-1">Close (keeps draft)</Button>
            <Button variant="emerald" onClick={save} disabled={saving || !canSave} className="flex-[2]">
              {saving ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
              {editing ? 'Save skill' : 'Create skill'}
            </Button>
          </div>
        </div>
      </BottomSheet>

      <ConfirmModal open={askReset} title="Reset this draft?"
        body="Clears everything you typed in this sheet and starts blank. This cannot be undone."
        confirmLabel="Reset draft" busy={false}
        onCancel={() => setAskReset(false)}
        onConfirm={doReset} />
    </>
  );
}
