import { useEffect, useRef, useState } from 'react';
import { Check, Loader2, Power, Pencil, Download, Trash2, Zap } from 'lucide-react';
import { api } from '../lib/api';
import type { Skill } from '../lib/types';
import { Button, Input, Card, Badge, IconBtn, Tip } from './ui';
import MarkdownEditor from './MarkdownEditor';
import { MarkdownView } from './Markdown';
import { cn } from '../lib/cn';

type SaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'error';

interface LiveForm {
  name: string;
  description: string;
  category: string;
  tags: string;
  content: string;
}

function fromSkill(s: Skill): LiveForm {
  return {
    name: s.name, description: s.description,
    category: s.category || '', tags: s.tags || '', content: s.content,
  };
}

function valid(f: LiveForm) {
  return f.name.trim() !== '' && f.description.trim() !== '' && f.content.trim() !== '';
}

// Skill tab: rendered preview + live editing. Typing updates the preview
// instantly; changes autosave (debounced) — no Save button needed.
export default function SkillDetail({ skill, onChanged, onToggle, onDelete, onExport, onOpenSheet }: {
  skill: Skill;
  onChanged: (s: Skill) => void;
  onToggle: (s: Skill) => void;
  onDelete: (s: Skill) => void;
  onExport: (s: Skill) => void;
  onOpenSheet: (s: Skill) => void;
}) {
  const [form, setForm] = useState<LiveForm>(() => fromSkill(skill));
  const [state, setState] = useState<SaveState>('idle');
  const [err, setErr] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const prevId = useRef(skill.id);

  // Re-init on skill switch. For same-skill external updates
  // (toggle/move/sheet-save) only sync while clean, so in-progress
  // typing is never clobbered. After our own save the values are
  // identical, so syncing is a harmless no-op.
  useEffect(() => {
    if (prevId.current !== skill.id) {
      prevId.current = skill.id;
      setForm(fromSkill(skill));
      setState('idle');
      setErr('');
      return;
    }
    if (state === 'idle' || state === 'saved') {
      setForm(fromSkill(skill));
    }
  }, [skill.id, skill.updatedAt]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  function patch<K extends keyof LiveForm>(k: K, v: LiveForm[K]) {
    setForm((f) => ({ ...f, [k]: v }));
    setState('dirty');
  }

  async function normalizeName() {
    try {
      const n = String(await api.NormalizeName(form.name));
      if (n && n !== form.name) patch('name', n);
    } catch {}
  }

  // debounced autosave
  useEffect(() => {
    if (state !== 'dirty') return;
    if (!valid(form)) return;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(async () => {
      setState('saving');
      setErr('');
      try {
        const saved = (await api.UpdateSkill(skill.id, {
          name: form.name.toLowerCase().trim(),
          description: form.description.trim(),
          content: form.content,
          category: form.category.trim(),
          tags: form.tags.trim(),
          scope: skill.scope,
          projectId: skill.projectId,
          enabled: skill.enabled,
        })) as Skill;
        onChanged(saved);
        setState('saved');
        setTimeout(() => setState((s) => (s === 'saved' ? 'idle' : s)), 2200);
      } catch (e: any) {
        setErr(e?.message || String(e));
        setState('error');
      }
    }, 900);
    return () => { if (timer.current) clearTimeout(timer.current); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form, state]);

  function statusPill() {
    switch (state) {
      case 'dirty':
        return <span className="font-mono text-[10px] text-amber-300">editing…</span>;
      case 'saving':
        return <span className="flex items-center gap-1 font-mono text-[10px] text-zinc-400"><Loader2 size={10} className="animate-spin" /> saving…</span>;
      case 'saved':
        return <span className="flex items-center gap-1 font-mono text-[10px] text-emerald-300"><Check size={10} /> saved ✓ live on MCP</span>;
      case 'error':
        return <span className="font-mono text-[10px] text-red-300">save failed</span>;
      default:
        return <span className="flex items-center gap-1 font-mono text-[10px] text-zinc-600"><Zap size={10} /> live edit · autosaves</span>;
    }
  }

  return (
    <div className="mx-auto grid max-w-6xl gap-3">
      <Card className="flex flex-wrap items-center gap-2 p-3">
        <code className="min-w-0 flex-1 truncate font-mono text-[15px] text-white">{skill.name}</code>
        {skill.scope === 'project'
          ? <Badge tone="indigo">project · {skill.projectSlug}</Badge>
          : <Badge tone="green">global · main MCP</Badge>}
        {statusPill()}
        <div className="ms-auto flex items-center gap-1.5">
          <Tip label={skill.enabled ? 'Disable (hide MCP tool)' : 'Enable (publish MCP tool)'}>
            <button onClick={() => onToggle(skill)}
              className={cn('flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[11px]',
                skill.enabled ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-200' : 'border-zinc-700 text-zinc-400 hover:bg-zinc-800')}>
              <Power size={12} /> {skill.enabled ? 'Enabled' : 'Disabled'}
            </button>
          </Tip>
          <Tip label="Full edit in sheet (scope, project, draft)">
            <Button variant="outline" className="!px-2.5 !py-1.5 text-[11px]" onClick={() => onOpenSheet(skill)}><Pencil size={12} /></Button>
          </Tip>
          <Tip label="Download as markdown">
            <Button variant="outline" className="!px-2.5 !py-1.5 text-[11px]" onClick={() => onExport(skill)}><Download size={12} /></Button>
          </Tip>
          <IconBtn title={`Delete ${skill.name}`} tip="Delete skill" variant="outline" className="hover:!text-red-300" onClick={() => onDelete(skill)}>
            <Trash2 size={14} />
          </IconBtn>
        </div>
      </Card>

      <div className="grid items-start gap-3 xl:grid-cols-2">
        <Card className="grid gap-3 p-4">
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Tool name · renames the MCP tool</div>
            <Input value={form.name} onChange={(e) => patch('name', e.target.value.toLowerCase())} onBlur={normalizeName}
              spellCheck={false} className="font-mono" />
          </div>
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Description · the AI reads this</div>
            <Input value={form.description} onChange={(e) => patch('description', e.target.value)} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Category</div>
              <Input value={form.category} onChange={(e) => patch('category', e.target.value)} spellCheck={false} className="font-mono" />
            </div>
            <div>
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Tags</div>
              <Input value={form.tags} onChange={(e) => patch('tags', e.target.value)} spellCheck={false} className="font-mono" />
            </div>
          </div>
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Content · markdown</div>
            <MarkdownEditor value={form.content} onChange={(v) => patch('content', v)} heightClass="h-[52vh]" />
          </div>
          {(!valid(form)) && (
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
              Name, description and content are required before autosave runs.
            </div>
          )}
          {err && <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-300">{err}</div>}
          <div className="text-[11px] text-zinc-600">
            Scope & project move via sidebar drag-drop or the sheet editor.
          </div>
        </Card>

        <div className="grid gap-3">
          <Card className="p-4">
            <div className="mb-2 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Live preview · what the AI sees</div>
            <div className="h-[52vh] overflow-y-auto rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2">
              <MarkdownView content={form.content} />
            </div>
          </Card>
          <Card className="p-3">
            <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-zinc-500">How the AI calls it</div>
            <pre className="overflow-x-auto whitespace-pre-wrap rounded-lg codeblock bg-black/60 p-2.5 font-mono text-[11px] leading-relaxed text-emerald-200/90">
              {skill.scope === 'project'
                ? `// project MCP only: SkillsMCP mcp --project ${skill.projectSlug}\n{"name": "list_skills", "arguments": {}}\n{"name": "${form.name || skill.name}", "arguments": {}}`
                : `{"name": "list_skills", "arguments": {}}\n{"name": "${form.name || skill.name}", "arguments": {}}\n{"name": "get_skill", "arguments": {"name": "${form.name || skill.name}"}}`}
            </pre>
          </Card>
        </div>
      </div>
    </div>
  );
}
