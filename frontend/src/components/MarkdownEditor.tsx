import { useRef } from 'react';
import { Bold, Italic, Code2, Heading2, List, ListOrdered, Quote, Link2 } from 'lucide-react';
import { Tip } from './ui';

// Toolbar markdown editor (Write-only — the rendered preview lives in the
// pane beside it). Toolbar edits operate on the textarea selection.
export default function MarkdownEditor({ value, onChange, heightClass = 'h-[52vh]' }: {
  value: string; onChange: (v: string) => void; heightClass?: string;
}) {
  const areaRef = useRef<HTMLTextAreaElement>(null);

  function applyEdit(fn: (text: string, selStart: number, selEnd: number) => { text: string; selStart: number; selEnd: number }) {
    const el = areaRef.current;
    if (!el) return;
    const out = fn(value, el.selectionStart, el.selectionEnd);
    onChange(out.text);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(out.selStart, out.selEnd);
    });
  }

  function wrap(before: string, after = '') {
    applyEdit((text, a, b) => {
      const sel = text.slice(a, b) || 'text';
      const next = text.slice(0, a) + before + sel + after + text.slice(b);
      return { text: next, selStart: a + before.length, selEnd: a + before.length + sel.length };
    });
  }

  function linePrefix(prefix: string) {
    applyEdit((text, a, b) => {
      const ls = text.lastIndexOf('\n', a - 1) + 1;
      let le = text.indexOf('\n', b);
      if (le === -1) le = text.length;
      const block = text.slice(ls, le).split('\n').map((l) => (l.startsWith(prefix) ? l.slice(prefix.length) : prefix + l)).join('\n');
      const next = text.slice(0, ls) + block + text.slice(le);
      return { text: next, selStart: ls, selEnd: ls + block.length };
    });
  }

  const tools = [
    { icon: Heading2, label: 'Heading', fn: () => linePrefix('## ') },
    { icon: Bold, label: 'Bold', fn: () => wrap('**', '**') },
    { icon: Italic, label: 'Italic', fn: () => wrap('*', '*') },
    { icon: Code2, label: 'Inline code', fn: () => wrap('`', '`') },
    { icon: Quote, label: 'Quote', fn: () => linePrefix('> ') },
    { icon: List, label: 'Bullet list', fn: () => linePrefix('- ') },
    { icon: ListOrdered, label: 'Numbered list', fn: () => linePrefix('1. ') },
    { icon: Link2, label: 'Link', fn: () => wrap('[', '](https://)') },
  ];

  return (
    <div className="overflow-hidden rounded-lg border border-zinc-700">
      <div className="flex items-center gap-0.5 border-b border-zinc-800 bg-zinc-900 px-1.5 py-1">
        {tools.map((t, i) => (
          <Tip key={i} label={t.label}>
            <button type="button" onClick={t.fn}
              className="grid h-7 w-7 place-items-center rounded-md text-zinc-500 transition hover:bg-zinc-800 hover:text-zinc-100">
              <t.icon size={13} />
            </button>
          </Tip>
        ))}
        <span className="ms-auto font-mono text-[10px] text-zinc-600">{value.length} chars</span>
      </div>
      <textarea ref={areaRef} value={value} onChange={(e) => onChange(e.target.value)} spellCheck={false}
        placeholder={'# My skill\n\nStep-by-step instruction the AI follows…\n\n## Steps\n\n1. First do…\n2. Then do…'}
        className={`w-full resize-none bg-zinc-950 px-3 py-2 font-mono text-[12px] leading-relaxed text-zinc-100 outline-none placeholder:text-zinc-600 ${heightClass}`} />
    </div>
  );
}
