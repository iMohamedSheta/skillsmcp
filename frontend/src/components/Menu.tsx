import { useEffect, useState } from 'react';
import { Plus, Copy, Check, LayoutGrid, BookOpen, FolderKanban, Plug2, Settings2, Minus, Square, X, ScrollText, GitCompareArrows, Layers } from 'lucide-react';
import { cn } from '../lib/cn';
import type { Tab } from './menuTypes';

export const DRAG = { ['--wails-draggable' as any]: 'drag' };
export const NODRAG = { ['--wails-draggable' as any]: 'no-drag' };

function WindowControls() {
  async function safe(fnName: string) {
    try {
      const mod = await import('../../wailsjs/runtime/runtime').catch(() => null as any);
      if (fnName === 'min') mod?.WindowMinimise?.();
      if (fnName === 'max') mod?.WindowToggleMaximise?.();
      if (fnName === 'quit') mod?.Quit?.();
    } catch {}
  }
  const base = 'flex h-9 w-11 items-center justify-center text-zinc-500 transition-colors';
  return (
    <div className="flex items-stretch" style={NODRAG}>
      <button title="Minimize" onClick={() => safe('min')} className={cn(base, 'hover:bg-zinc-800 hover:text-zinc-100')}>
        <Minus size={14} />
      </button>
      <button title="Maximize / restore" onClick={() => safe('max')} className={cn(base, 'hover:bg-zinc-800 hover:text-zinc-100')}>
        <Square size={11} />
      </button>
      <button title="Close" onClick={() => safe('quit')} className={cn(base, 'hover:bg-[#E81123] hover:text-white')}>
        <X size={15} />
      </button>
    </div>
  );
}

function Item({ icon: Icon, label, shortcut, checked, onClick }: {
  icon?: any; label: string; shortcut?: string; checked?: boolean; onClick?: () => void;
}) {
  return (
    <button type="button" onClick={onClick}
      className="dyp flex w-full cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-[7px] text-[13px] text-zinc-200/90 transition-colors hover:bg-zinc-800 hover:text-zinc-50">
      <span className="flex w-4 justify-center text-zinc-500">
        {checked ? <Check size={14} className="acc-text" /> : Icon ? <Icon size={14} /> : null}
      </span>
      <span className="flex-1 text-start font-medium">{label}</span>
      {shortcut && (
        <kbd className="rounded border border-zinc-700 bg-zinc-950 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500">{shortcut}</kbd>
      )}
    </button>
  );
}

function Sep() {
  return <div className="mx-2 my-1 h-px bg-zinc-800" />;
}

export default function Menubar({ tab, onTab, onAdd, onCopyMCP, onOpenSettings, onToggleSidebar }: {
  tab: Tab;
  onTab: (t: Tab) => void;
  onAdd: () => void;
  onCopyMCP: () => void;
  onOpenSettings: () => void;
  onToggleSidebar: () => void;
}) {
  const [open, setOpen] = useState<string | null>(null);
  const close = () => setOpen(null);

  useEffect(() => {
    if (!open) return;
    const fn = (e: KeyboardEvent) => { if (e.key === 'Escape') close(); };
    document.addEventListener('keydown', fn);
    return () => document.removeEventListener('keydown', fn);
  }, [open ]);

  const run = (fn?: () => void) => () => { close(); fn?.(); };

  const tabs: { id: Tab; label: string; icon: any }[] = [
    { id: 'home', label: 'Home', icon: LayoutGrid },
    { id: 'skills', label: 'Skill', icon: BookOpen },
    { id: 'projects', label: 'Projects', icon: FolderKanban },
    { id: 'workspace', label: 'Workspace', icon: Layers },
    { id: 'sync', label: 'Sync', icon: GitCompareArrows },
    { id: 'mcp', label: 'MCP', icon: Plug2 },
    { id: 'logs', label: 'Logs', icon: ScrollText },
  ];

  const menus = [
    {
      key: 'skills', label: 'Skills',
      items: [
        { icon: Plus, label: 'New Skill…', shortcut: 'Ctrl+N', onClick: run(onAdd) },
        { type: 'sep' },
        { icon: Copy, label: 'Copy MCP config', onClick: run(onCopyMCP) },
      ],
    },
    {
      key: 'view', label: 'View',
      items: tabs.map((t) => ({ icon: t.icon, label: t.label, checked: tab === t.id, onClick: run(() => onTab(t.id)) })),
    },
    {
      key: 'settings', label: 'Settings',
      items: [
        { icon: Settings2, label: 'Appearance…', onClick: run(onOpenSettings) },
      ],
    },
  ];

  return (
    <div className="bar-bg relative z-30 flex h-9 shrink-0 select-none items-center gap-0.5 border-b border-zinc-800 ps-2"
      style={DRAG}>
      {open && <div className="fixed inset-0 z-40 cursor-default" style={NODRAG} onClick={close} />}
      <button type="button" onClick={onToggleSidebar} title="Toggle sidebar" style={NODRAG}
        className="grid h-8 w-8 shrink-0 cursor-pointer place-items-center rounded-md text-zinc-500 transition active:scale-95 hover:bg-zinc-800 hover:text-zinc-200">
        <LayoutGrid size={14} />
      </button>
      <div className="mx-1 h-4 w-px bg-zinc-800" />
      {menus.map((m) => (
        <div key={m.key} className="relative" style={NODRAG}>
          <button type="button" onClick={() => setOpen(open === m.key ? null : m.key)}
            onMouseEnter={() => open && open !== m.key && setOpen(m.key)}
            className={cn('cursor-pointer rounded-md px-3 py-1.5 text-[13px] font-medium transition-colors',
              open === m.key ? 'bg-zinc-800 text-zinc-50' : 'text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-100')}>
            {m.label}
          </button>
          {open === m.key && (
            <div className="pop-in absolute start-0 top-full z-50 mt-1 w-60 rounded-xl border border-zinc-700 bg-zinc-900 p-1.5 shadow-2xl">
              {m.items.map((it: any, i: number) => it.type === 'sep' ? <Sep key={i} /> : <Item key={i} {...it} />)}
            </div>
          )}
        </div>
      ))}
      <div className="ms-auto flex items-center gap-1 pe-0" style={NODRAG}>
        <button type="button" onClick={onOpenSettings} title="Settings"
          className="grid h-8 w-8 shrink-0 cursor-pointer place-items-center rounded-md text-zinc-500 transition active:scale-95 hover:bg-zinc-800 hover:text-zinc-200">
          <Settings2 size={14} />
        </button>
        <WindowControls />
      </div>
    </div>
  );
}
