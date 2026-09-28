import { useState } from 'react';
import { Palette, Sun, Moon, Check } from 'lucide-react';
import { Sheet, Button } from './ui';
import { ACCENTS, type Appearance } from '../lib/appearance';
import { cn } from '../lib/cn';

export default function SettingsSheet({ open, onClose, appearance, onPatch, storePath, version }: {
  open: boolean; onClose: () => void;
  appearance: Appearance; onPatch: (p: Partial<Appearance>) => void;
  storePath: string; version: string;
}) {
  const [tab, setTab] = useState<'appearance' | 'data'>('appearance');
  return (
    <Sheet open={open} onClose={onClose} title="Settings" subtitle={`SkillsMCP ${version} · personal skill library`}>
      <div className="p-4">
        <div className="mb-3 flex gap-1 rounded-lg border border-zinc-800 bg-zinc-900 p-1">
          {(['appearance', 'data'] as const).map((t) => (
            <button key={t} onClick={() => setTab(t)}
              className={cn('flex-1 rounded-md px-3 py-1.5 text-xs capitalize',
                tab === t ? 'bg-zinc-700/60 text-white' : 'text-zinc-400 hover:text-zinc-100')}>
              {t}
            </button>
          ))}
        </div>

        {tab === 'appearance' && (
          <div className="grid gap-4">
            <div>
              <div className="mb-1.5 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Theme</div>
              <div className="flex gap-1.5">
                {[
                  { v: 'dark', icon: Moon, label: 'Dark' },
                  { v: 'light', icon: Sun, label: 'Light' },
                ].map((o) => (
                  <button key={o.v} onClick={() => onPatch({ theme: o.v })}
                    className={cn('flex flex-1 items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs',
                      appearance.theme === o.v ? 'acc-border acc-soft acc-text border' : 'border-zinc-800 text-zinc-400 hover:bg-zinc-800')}>
                    <o.icon size={13} /> {o.label}
                    {appearance.theme === o.v && <Check size={12} />}
                  </button>
                ))}
              </div>
            </div>
            <div>
              <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-zinc-500">
                <Palette size={11} /> Accent
              </div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(ACCENTS).map(([name, rgb]) => (
                  <button key={name} title={name} onClick={() => onPatch({ accent: name })}
                    className={cn('h-8 w-8 rounded-lg border-2 transition active:scale-95',
                      appearance.accent === name ? 'border-white/80' : 'border-transparent hover:scale-105')}
                    style={{ backgroundColor: `rgb(${rgb})` }} />
                ))}
              </div>
              <div className="mt-1 font-mono text-[10px] text-zinc-600">active: {appearance.accent}</div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              {([
                ['radius', ['balanced', 'sharp', 'soft', 'round', 'none']],
                ['density', ['compact', 'comfortable', 'spacious']],
                ['cardStyle', ['elevated', 'bordered', 'glass', 'flat']],
                ['codeFont', ['jetbrains', 'fira', 'cascadia', 'system']],
              ] as const).map(([key, opts]) => (
                <div key={key}>
                  <div className="mb-1.5 text-[11px] font-medium uppercase tracking-wider text-zinc-500">{key}</div>
                  <div className="grid gap-1">
                    {opts.map((o) => (
                      <button key={o} onClick={() => onPatch({ [key]: o } as any)}
                        className={cn('rounded-md border px-2 py-1 text-left text-[11px]',
                          (appearance as any)[key] === o
                            ? 'acc-border acc-soft acc-text border'
                            : 'border-zinc-800 text-zinc-400 hover:bg-zinc-800')}>
                        {o}
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {tab === 'data' && (
          <div className="grid gap-3 text-xs text-zinc-400">
            <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3">
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">Database</div>
              <div className="font-mono text-[11px] text-zinc-300">{storePath || 'loading…'}</div>
              <div className="mt-1 leading-relaxed">SQLite WAL. Every skill you create is a row here — and an MCP tool.</div>
            </div>
            <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3">
              <div className="mb-1 text-[11px] font-medium uppercase tracking-wider text-zinc-500">How tools map</div>
              <div className="leading-relaxed">
                <code className="font-mono text-emerald-300">list_skills</code> = index of enabled skills.
                <br /><code className="font-mono text-emerald-300">get_skill(name)</code> = full markdown for one skill.
                <br /><code className="font-mono text-emerald-300">&lt;skill-name&gt;</code> = shortcut tool per skill (same content).
              </div>
            </div>
            <Button variant="outline" onClick={onClose}>Done</Button>
          </div>
        )}
      </div>
    </Sheet>
  );
}
