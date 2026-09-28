import { useEffect, useRef, useState } from 'react';
import { Check, Upload, FileText, X, Globe, FolderKanban, Loader2, TriangleAlert } from 'lucide-react';
import { api } from '../lib/api';
import type { Project } from '../lib/types';
import { BottomSheet, Button, Input, Tip } from './ui';
import { cn } from '../lib/cn';

interface ImportRow {
  key: string;
  fileName: string;
  name: string;
  description: string;
  content: string;
  status: 'ready' | 'ok' | 'error';
  msg: string;
}

function slugify(s: string) {
  return s.toLowerCase().trim().replace(/[\s_]+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '').slice(0, 64);
}

function parseFile(fileName: string, text: string): { name: string; description: string } {
  const lines = text.split('\n');
  let title = '', desc = '';
  for (const ln of lines) {
    if (!title && ln.startsWith('# ')) { title = ln.slice(2).trim(); continue; }
    if (!desc && ln.trim().startsWith('>')) { desc = ln.trim().replace(/^>\s?/, ''); continue; }
  }
  const base = fileName.replace(/\.[^.]+$/, '');
  return { name: slugify(title || base), description: desc };
}

let rowSeq = 0;

// Bulk import: one skill per .md/.txt file, into Global or a project.
// Name comes from the first `# heading` (fallback: filename),
// description from the first `> quote` — both editable per row.
export default function ImportSheet({ open, onClose, initialScope, initialProjectId, projects, onImported }: {
  open: boolean; onClose: () => void;
  initialScope: 'global' | 'project'; initialProjectId: string;
  projects: Project[];
  onImported: () => void;
}) {
  const [rows, setRows] = useState<ImportRow[]>([]);
  const [scope, setScope] = useState<'global' | 'project'>(initialScope);
  const [projectId, setProjectId] = useState(initialProjectId);
  const [importing, setImporting] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setScope(initialScope);
      setProjectId(initialProjectId);
    }
  }, [open, initialScope, initialProjectId ]);

  async function addFiles(list: FileList | File[]) {
    const files = [...list].filter((f) => !f.name.startsWith('.'));
    for (const f of files) {
      try {
        const text = await f.text();
        if (!text.trim()) continue;
        const p = parseFile(f.name, text);
        setRows((r) => [...r, {
          key: `row-${++rowSeq}-${Date.now()}`,
          fileName: f.name, name: p.name, description: p.description,
          content: text, status: 'ready', msg: '',
        }]);
      } catch {}
    }
  }

  function patchRow(key: string, p: Partial<ImportRow>) {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...p, status: r.status === 'ok' ? 'ok' : 'ready' as const, msg: '' } : r)));
  }

  const pending = rows.filter((r) => r.status !== 'ok');
  const done = rows.filter((r) => r.status === 'ok').length;
  const canImport = !importing && pending.length > 0
    && pending.every((r) => r.name.trim() !== '' && r.description.trim() !== '' && r.content.trim() !== '')
    && (scope !== 'project' || projectId !== '');

  async function doImport() {
    if (!canImport) return;
    setImporting(true);
    for (const r of pending) {
      try {
        await api.CreateSkill({
          name: r.name.toLowerCase().trim(),
          description: r.description.trim(),
          content: r.content,
          category: '',
          tags: '',
          scope, projectId: scope === 'project' ? projectId : '',
          enabled: true,
        });
        setRows((rs) => rs.map((x) => (x.key === r.key ? { ...x, status: 'ok' as const, msg: '' } : x)));
      } catch (e: any) {
        setRows((rs) => rs.map((x) => (x.key === r.key ? { ...x, status: 'error' as const, msg: e?.message || String(e) } : x)));
      }
    }
    setImporting(false);
    onImported();
  }

  return (
    <BottomSheet open={open} onClose={onClose} wide
      title="Import skills from files"
      subtitle="One skill per .md file · Global = main MCP · Project = that project's own MCP">
      <div className="grid gap-3 p-4">
        {/* drop zone */}
        <div
          onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => { e.preventDefault(); setDragOver(false); if (e.dataTransfer.files?.length) void addFiles(e.dataTransfer.files); }}
          onClick={() => fileRef.current?.click()}
          className={cn('flex cursor-pointer items-center gap-3 rounded-xl border border-dashed px-4 py-5 transition',
            dragOver ? 'acc-border acc-soft border' : 'border-zinc-700 hover:border-zinc-500 hover:bg-zinc-900')}>
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-500/15 text-emerald-300"><Upload size={18} /></div>
          <div className="min-w-0">
            <div className="text-[13px] font-medium text-zinc-200">Drop .md files here or click to browse</div>
            <div className="text-[11px] text-zinc-500">Name ← first <code className="font-mono"># heading</code> (else filename) · description ← first <code className="font-mono">&gt; quote</code></div>
          </div>
          <input ref={fileRef} type="file" accept=".md,.markdown,.txt" multiple className="hidden"
            onChange={(e) => { if (e.target.files?.length) void addFiles(e.target.files); e.target.value = ''; }} />
        </div>

        {/* scope switch */}
        <div className="grid grid-cols-2 gap-1 rounded-lg border border-zinc-800 bg-zinc-900 p-1">
          {([
            { v: 'global', icon: Globe, label: 'Global', hint: 'main MCP' },
            { v: 'project', icon: FolderKanban, label: 'Project', hint: 'project MCP' },
          ] as const).map((o) => (
            <button key={o.v} onClick={() => setScope(o.v)}
              className={cn('flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-xs transition',
                scope === o.v ? 'acc-bg acc-on font-semibold' : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100')}>
              <o.icon size={13} /> {o.label}
              <span className={cn('font-mono text-[10px]', scope === o.v ? 'opacity-70' : 'text-zinc-600')}>{o.hint}</span>
            </button>
          ))}
        </div>

        {scope === 'project' && (
          <div>
            <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Target project</div>
            {projects.length === 0 ? (
              <div className="rounded-lg border border-dashed border-zinc-700 p-3 text-center text-[11px] text-zinc-500">
                No projects yet — create one in the Projects tab first.
              </div>
            ) : (
              <div className="grid max-h-36 gap-1 overflow-y-auto">
                {projects.map((p) => (
                  <button key={p.id} onClick={() => setProjectId(p.id)}
                    className={cn('flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-xs transition',
                      projectId === p.id ? 'acc-border acc-soft border' : 'border-zinc-800 hover:bg-zinc-800')}>
                    <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: p.color || '#52525b' }} />
                    <span className="font-medium text-zinc-100">{p.name}</span>
                    <span className="truncate font-mono text-[10px] text-zinc-500">skillsmcp-{p.slug}</span>
                    {projectId === p.id && <Check size={13} className="acc-text ml-auto shrink-0" />}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        {/* file rows */}
        {rows.length > 0 && (
          <div className="grid gap-1.5">
            <div className="flex items-center text-[11px] font-medium uppercase tracking-wider text-zinc-500">
              Files · {rows.length}
              {done > 0 && <span className="acc-text ml-1.5 font-mono normal-case">· {done} imported ✓</span>}
              {done > 0 && (
                <button onClick={() => setRows((rs) => rs.filter((r) => r.status !== 'ok'))}
                  className="ml-auto text-zinc-500 normal-case underline hover:text-zinc-200">clear finished</button>
              )}
            </div>
            {rows.map((r) => (
              <div key={r.key} className={cn('grid gap-1.5 rounded-lg border p-2.5',
                r.status === 'ok' ? 'border-emerald-500/30 bg-emerald-500/5'
                : r.status === 'error' ? 'border-red-500/30 bg-red-500/5'
                : 'border-zinc-800 bg-zinc-950')}>
                <div className="flex items-center gap-1.5">
                  {r.status === 'ok'
                    ? <Check size={13} className="shrink-0 text-emerald-400" />
                    : r.status === 'error'
                      ? <TriangleAlert size={13} className="shrink-0 text-red-300" />
                      : <FileText size={13} className="shrink-0 text-zinc-500" />}
                  <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-400">{r.fileName}</span>
                  <span className="shrink-0 font-mono text-[10px] text-zinc-600">{r.content.length} chars</span>
                  {r.status !== 'ok' && (
                    <Tip label="Remove from list">
                      <button onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))}
                        className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-zinc-600 hover:bg-zinc-800 hover:text-red-300">
                        <X size={12} />
                      </button>
                    </Tip>
                  )}
                </div>
                {r.status !== 'ok' && (
                  <div className="grid gap-1.5 md:grid-cols-2">
                    <Input value={r.name} onChange={(e) => patchRow(r.key, { name: e.target.value.toLowerCase() })}
                      placeholder="tool-name" spellCheck={false} className="!py-1.5 font-mono text-[12px]" />
                    <Input value={r.description} onChange={(e) => patchRow(r.key, { description: e.target.value })}
                      placeholder="Description (required — the AI reads it)" className="!py-1.5 text-[12px]" />
                  </div>
                )}
                {r.status === 'error' && <div className="text-[11px] text-red-300">{r.msg}</div>}
              </div>
            ))}
          </div>
        )}

        <div className="sticky bottom-0 flex gap-2 border-t border-zinc-800 bg-zinc-950/95 py-3 backdrop-blur">
          <Button variant="ghost" onClick={onClose} className="flex-1">Close</Button>
          <Button variant="emerald" onClick={doImport} disabled={!canImport} className="flex-[2]">
            {importing ? <Loader2 size={14} className="animate-spin" /> : <Upload size={14} />}
            {importing ? 'Importing…' : `Import ${pending.length} skill${pending.length === 1 ? '' : 's'}`}
          </Button>
        </div>
      </div>
    </BottomSheet>
  );
}
