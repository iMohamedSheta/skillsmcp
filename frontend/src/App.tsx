import { useEffect, useMemo, useRef, useState } from 'react';
import {
  LayoutGrid, BookOpen, FolderKanban, Plus, Search, Plug2, RefreshCw, Trash2, Copy, Check,
  Upload, Power, ScrollText, Pencil, FlaskConical, Globe,
  ChevronDown, ChevronRight, FolderPlus,
} from 'lucide-react';
import { api, type UpdateInfo } from './lib/api';
import type { Project, ProjectInput, Skill, SkillInput } from './lib/types';
import { applyAppearance, loadLocalAppearance, mergeSettingsMap, saveLocalAppearance, type Appearance } from './lib/appearance';
import Menubar from './components/Menu';
import SettingsSheet from './components/SettingsSheet';
import UpdateBanner from './components/UpdateBanner';
import McpPanel from './components/McpPanel';
import SkillSheet, { EMPTY_SKILL } from './components/SkillSheet';
import ImportSheet from './components/ImportSheet';
import SkillDetail from './components/SkillDetail';
import ProjectSheet, { EMPTY_PROJECT } from './components/ProjectSheet';
import type { Tab } from './components/menuTypes';
import { Badge, Button, Card, Empty, IconBtn, Input, ConfirmModal, Tip } from './components/ui';
import { cn } from './lib/cn';

type HomeFilter = 'all' | 'global' | string; // 'all' | 'global' | `project:<id>`

export default function App() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [activeId, setActiveId] = useState('');
  const [tab, setTab] = useState<Tab>('home');
  const [query, setQuery] = useState('');
  const [homeFilter, setHomeFilter] = useState<HomeFilter>('all');
  const [mcpUrl, setMcpUrl] = useState('http://127.0.0.1:9423');
  const [preview, setPreview] = useState('');
  const [storePath, setStorePath] = useState('');
  const [version, setVersion] = useState('dev');
  // self-update via GitHub Releases
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updateBanner, setUpdateBanner] = useState(false);
  const [updateChecking, setUpdateChecking] = useState(false);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateMsg, setUpdateMsg] = useState('');
  const [updateProgress, setUpdateProgress] = useState<{ written: number; total: number } | null>(null);
  const updateChecked = useRef(false);
  const [appearance, setAppearance] = useState<Appearance>(() => loadLocalAppearance());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [sideOpen, setSideOpen] = useState(true);
  const [confirm, setConfirm] = useState<{ title: string; body: string; confirmLabel: string; action: () => Promise<void> } | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const [logPath, setLogPath] = useState('');
  const [mcpConfig, setMcpConfig] = useState('');
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState('');
  // sidebar drag-drop (ReadGate-style): skill id being dragged + drop target id
  const [dragSkill, setDragSkill] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [showNewProject, setShowNewProject] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  // bottom sheet for skills (drafts remembered) + projects
  const [skillSheet, setSkillSheet] = useState<{ open: boolean; editing: Skill | null; initial: SkillInput }>({ open: false, editing: null, initial: { ...EMPTY_SKILL } });
  const [projectSheet, setProjectSheet] = useState<{ open: boolean; editing: Project | null; initial: ProjectInput }>({ open: false, editing: null, initial: { ...EMPTY_PROJECT } });
  const [projCfg, setProjCfg] = useState<Record<string, string>>({});
  const [projTest, setProjTest] = useState<Record<string, string>>({});
  // bulk file import (global or into a project)
  const [importSheet, setImportSheet] = useState<{ open: boolean; scope: 'global' | 'project'; projectId: string }>({ open: false, scope: 'global', projectId: '' });

  function usePanelWidth(key: string, def: number, min: number, max: number) {
    const [w, setW] = useState(() => {
      const v = Number(localStorage.getItem('skillsmcp-' + key));
      return Number.isFinite(v) && v >= min && v <= max ? v : def;
    });
    const start = (e: React.MouseEvent) => {
      e.preventDefault();
      const x0 = e.clientX, w0 = w;
      const mv = (ev: MouseEvent) => setW(Math.min(max, Math.max(min, w0 + ev.clientX - x0)));
      const up = () => {
        window.removeEventListener('mousemove', mv);
        window.removeEventListener('mouseup', up);
        setW((cur) => {
          try {
            localStorage.setItem('skillsmcp-' + key, String(cur));
            api.SetSetting('ui.' + key, String(cur)).catch(() => {});
          } catch {}
          return cur;
        });
      };
      window.addEventListener('mousemove', mv);
      window.addEventListener('mouseup', up);
    };
    return [w, start] as const;
  }
  const [sideW, startSide] = usePanelWidth('sidebarWidth', 280, 220, 460);

  const active = useMemo(() => skills.find((s) => s.id === activeId) || null, [skills, activeId]);
  const globals = useMemo(() => skills.filter((s) => s.scope !== 'project'), [skills]);

  async function refresh(selectId?: string) {
    try {
      const [list, projs] = await Promise.all([
        api.ListSkills() as Promise<Skill[]>,
        api.ListProjects() as Promise<Project[]>,
      ]);
      setSkills(list || []);
      setProjects(projs || []);
      const want = selectId || activeId;
      if (want && (list || []).some((s) => s.id === want)) setActiveId(want);
      // NOTE: no auto-focus — a fresh library opens with nothing selected.
      const m = (await api.MCPStatus()) as any;
      setMcpUrl(m?.url || 'http://127.0.0.1:9423');
      setPreview(String(await api.MCPToolsPreview()));
      setStorePath(String(await api.StorePath()));
      try { setMcpConfig(String(await api.OpencodeConfig())); } catch {}
    } catch (e: any) {
      setErr(e?.message || String(e));
    }
  }

  useEffect(() => { refresh(); }, []);
  useEffect(() => { api.Version().then((v: any) => { if (v) setVersion(String(v)); }).catch(() => {}); }, []);
  useEffect(() => { applyAppearance(appearance); }, [appearance]);
  useEffect(() => {
    api.GetSettings().then((m: any) => {
      if (!m) return;
      setAppearance((prev) => {
        const merged = mergeSettingsMap(prev, m as Record<string, string>);
        saveLocalAppearance(merged);
        return merged;
      });
    }).catch(() => {});
  }, []);
  useEffect(() => { if (tab === 'logs') reloadLogs(); }, [tab ]);
  useEffect(() => { if (settingsOpen) reloadLogs(); }, [settingsOpen ]);

  function patchAppearance(p: Partial<Appearance>) {
    setAppearance((prev) => {
      const next = { ...prev, ...p };
      saveLocalAppearance(next);
      (Object.keys(p) as (keyof Appearance)[]).forEach((k) => {
        api.SetSetting('appearance.' + k, next[k]).catch(() => {});
      });
      return next;
    });
  }

  async function reloadLogs() {
    try {
      setLogs(((await api.GetLogs(200)) as unknown as string[]) || []);
      setLogPath(String(await (api as any).LogPath()));
    } catch {}
  }

  async function clearLogs() {
    try {
      await api.ClearLogs();
      setLogs([]);
    } catch {}
  }

  // ---------- self-update via GitHub Releases ----------
  async function runUpdateCheck(manual: boolean) {
    if (updateChecking) return null;
    setUpdateChecking(true);
    if (manual) setUpdateMsg('');
    try {
      const info = (await api.CheckForUpdates()) as unknown as UpdateInfo;
      setUpdateInfo(info);
      if (info?.updateAvailable) {
        // honor "skip this version" for automatic popups, never for manual checks
        if (!manual) {
          try {
            const m = (await api.GetSettings()) as unknown as Record<string, string>;
            if (m?.['update.skipVersion'] === info.latestVersion) return info;
          } catch {}
        }
        setUpdateBanner(true);
      } else if (manual) {
        setUpdateMsg(`You're on the latest version (${info?.currentVersion || version}).`);
      }
      return info;
    } catch (e: any) {
      if (manual) setUpdateMsg('update check failed: ' + (e?.message || String(e)));
      return null;
    } finally {
      setUpdateChecking(false);
    }
  }

  // One automatic check per session, shortly after startup. Manual checks
  // (Settings → General → Updates) always hit the network. Auto checks are
  // throttled to once per 24h and skipped when the user disabled them.
  useEffect(() => {
    if (updateChecked.current) return;
    updateChecked.current = true;
    const h = setTimeout(async () => {
      try {
        const m = (await api.GetSettings()) as unknown as Record<string, string>;
        if (m?.['update.autoCheck'] === 'off') return;
        const last = Date.parse(m?.['update.lastCheck'] || '');
        if (Number.isFinite(last) && Date.now() - last < 24 * 3600 * 1000) return;
      } catch {}
      runUpdateCheck(false).catch(() => {});
    }, 4000);
    return () => clearTimeout(h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Download progress comes from the Go updater via Wails events
  // (window.runtime exists only inside the desktop webview — no-op in web dev).
  useEffect(() => {
    let off: (() => void) | undefined;
    try {
      const rt = (window as any).runtime;
      if (rt?.EventsOn) off = rt.EventsOn('update:progress', (p: any) => {
        const d = Array.isArray(p) ? p[0] : p;
        if (d && typeof d.written === 'number') setUpdateProgress({ written: d.written, total: d.total || 0 });
      });
    } catch {}
    return () => { try { off?.(); } catch {} };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function installUpdate() {
    if (updateBusy) return;
    setUpdateBusy(true);
    setUpdateProgress(null);
    setUpdateMsg('');
    try {
      const msg = String(await api.DownloadAndInstallUpdate());
      setUpdateMsg(msg);
    } catch (e: any) {
      setUpdateMsg('update failed: ' + (e?.message || String(e)));
    } finally {
      setUpdateBusy(false);
      setUpdateProgress(null);
    }
  }

  async function skipUpdateVersion() {
    if (updateInfo?.latestVersion) {
      try { await api.SkipUpdateVersion(updateInfo.latestVersion); } catch {}
    }
    setUpdateBanner(false);
  }

  async function openReleasePage() {
    try {
      const err = String(await api.OpenReleasePage(updateInfo?.pageUrl || ''));
      if (err) setUpdateMsg(err);
    } catch (e: any) {
      setUpdateMsg('could not open release page: ' + (e?.message || String(e)));
    }
  }

  // Skill navigation: clicking a skill focuses it and opens the Skill tab
  // (preview + live edit). Nothing is auto-focused on load.
  function goSkill(id: string) {
    setActiveId(id);
    setTab('skills');
  }

  function openNewSkill(scope: 'global' | 'project' = 'global', projectId = '') {
    setSkillSheet({ open: true, editing: null, initial: { ...EMPTY_SKILL, scope, projectId } });
  }

  function openSkillSheet(s: Skill) {
    setSkillSheet({ open: true, editing: s, initial: { ...EMPTY_SKILL } });
  }

  async function toggleEnabled(s: Skill) {
    try {
      await api.SetSkillEnabled(s.id, !s.enabled);
      await refresh(s.id);
    } catch (e: any) {
      setErr(e?.message || String(e));
    }
  }

  async function copyMCP() {
    try {
      await navigator.clipboard.writeText(String(await api.OpencodeConfig()));
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {}
  }

  function exportSkill(s: Skill) {
    const blob = new Blob([`# ${s.name}\n\n> ${s.description}\n\n${s.content}`], { type: 'text/markdown' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `${s.name || 'skill'}.md`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  async function loadProjCfg(slug: string) {
    if (projCfg[slug]) return projCfg[slug];
    const v = String(await api.OpencodeConfigForProject(slug));
    setProjCfg((c) => ({ ...c, [slug]: v }));
    return v;
  }

  // ---- filtering ----
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = skills;
    if (homeFilter === 'global') list = list.filter((s) => s.scope !== 'project');
    else if (homeFilter.startsWith('project:')) {
      const pid = homeFilter.slice(8);
      list = list.filter((s) => s.projectId === pid);
    }
    if (q) list = list.filter((s) => (s.name + ' ' + s.description + ' ' + s.category + ' ' + s.tags + ' ' + s.projectName).toLowerCase().includes(q));
    // Backend returns sort_order — preserve it (manual drag order, name tiebreak).
    return list;
  }, [skills, query, homeFilter]);

  const enabledCount = skills.filter((s) => s.enabled).length;

  function scopeBadge(s: Skill) {
    if (s.scope === 'project') {
      return <Badge tone="indigo">project{s.projectSlug ? ` · ${s.projectSlug}` : ''}</Badge>;
    }
    return <Badge tone="green">global</Badge>;
  }

  function skillCard(s: Skill) {
    const proj = projects.find((p) => p.id === s.projectId);
    return (
      <Card key={s.id} draggable
        title="Drag onto another card to reorder (moves scope too when dropped on another group)"
        onDragStart={(e) => {
          e.dataTransfer.setData('text/skillsmcp-skill', s.id);
          e.dataTransfer.effectAllowed = 'move';
          setDragSkill(s.id);
        }}
        onDragEnd={() => { setDragSkill(null); setDropTarget(null); }}
        onDragOver={(e) => {
          if (dragSkill && dragSkill !== s.id) {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
            setDropTarget(`order:${s.id}`);
          }
        }}
        onDragLeave={() => setDropTarget((t) => (t === `order:${s.id}` ? null : t))}
        onDrop={(e) => {
          e.preventDefault();
          const sid = e.dataTransfer.getData('text/skillsmcp-skill') || dragSkill;
          setDropTarget(null);
          setDragSkill(null);
          if (sid && sid !== s.id) void reorderSkill(sid, s.id);
        }}
        className={cn('flex cursor-grab flex-col active:cursor-grabbing',
          dragSkill === s.id && 'opacity-40',
          dropTarget === `order:${s.id}` && 'outline outline-1 outline-emerald-500/60')}>
        {s.scope === 'project' && <div className="h-1 rounded-t-xl" style={{ background: proj?.color || '#6366f1' }} />}
        <div className="flex flex-1 flex-col gap-1.5 p-3">
          <div className="flex items-start gap-1.5">
            <span className={cn('mt-1.5 h-2 w-2 shrink-0 rounded-full', s.enabled ? 'bg-emerald-400' : 'bg-zinc-600')} />
            <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-zinc-100">{s.name}</code>
            <Tip label={s.enabled ? 'Disable (hide MCP tool)' : 'Enable (publish MCP tool)'}>
              <button onClick={() => toggleEnabled(s)}
                className="shrink-0 rounded-md p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200">
                <Power size={13} className={s.enabled ? 'text-emerald-400' : 'text-zinc-600'} />
              </button>
            </Tip>
          </div>
          <div className="flex flex-wrap gap-1">{scopeBadge(s)}{s.category && <Badge tone="zinc">{s.category}</Badge>}</div>
          <div className="line-clamp-2 min-h-8 text-[12px] leading-relaxed text-zinc-400">{s.description}</div>
          <div className="font-mono text-[10px] text-zinc-600">{s.content.length} chars · {s.enabled ? 'live' : 'disabled'} · {s.updatedAt?.slice(0, 10)}</div>
          <div className="mt-auto flex gap-1 pt-1">
            <Button variant="outline" className="!px-2 !py-1 text-[11px]" onClick={() => goSkill(s.id)}>
              <Pencil size={11} /> Open
            </Button>
            <IconBtn title={`Delete ${s.name}`} tip="Delete skill" size="sm" className="ml-auto hover:!text-red-300"
              onClick={() => setConfirm({
                title: `Delete skill "${s.name}"?`,
                body: `Its MCP tool disappears on the next tools/list. This cannot be undone.`,
                confirmLabel: 'Delete skill',
                action: async () => { await api.DeleteSkill(s.id); if (activeId === s.id) setActiveId(''); await refresh(); },
              })}>
              <Trash2 size={13} />
            </IconBtn>
          </div>
        </div>
      </Card>
    );
  }

  function matchesQuery(s: Skill) {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return (s.name + ' ' + s.description + ' ' + s.category + ' ' + s.tags + ' ' + s.projectName).toLowerCase().includes(q);
  }

  async function moveSkill(skillId: string, scope: 'global' | 'project', projectId: string) {
    const s = skills.find((x) => x.id === skillId);
    if (!s) return;
    if (scope === 'global' && s.scope !== 'project') return;
    if (scope === 'project' && s.projectId === projectId) return;
    try {
      await api.MoveSkill(skillId, scope, projectId);
      await refresh(skillId);
    } catch (e: any) {
      setErr(e?.message || String(e));
    }
  }

  // Home-grid drop on a card: insert the dragged skill right before the
  // target card. Dropping across groups also moves scope (then positions).
  async function reorderSkill(draggedId: string, targetId: string) {
    const dragged = skills.find((x) => x.id === draggedId);
    const target = skills.find((x) => x.id === targetId);
    if (!dragged || !target || draggedId === targetId) return;
    try {
      const tScope = target.scope === 'project' ? 'project' : 'global';
      const tPid = target.scope === 'project' ? target.projectId : '';
      const dScope = dragged.scope === 'project' ? 'project' : 'global';
      const dPid = dragged.scope === 'project' ? dragged.projectId : '';
      if (tScope !== dScope || tPid !== dPid) {
        await api.MoveSkill(draggedId, tScope, tPid);
      }
      const order = skills.map((s) => s.id).filter((id) => id !== draggedId);
      const at = order.indexOf(targetId);
      order.splice(at < 0 ? order.length : at, 0, draggedId);
      await api.ReorderSkills(order);
      await refresh(draggedId);
    } catch (e: any) {
      setErr(e?.message || String(e));
    }
  }

  // ReadGate-style drop zone props: 'global' or a project id.
  function dropProps(targetId: string, isProject: boolean) {
    return {
      onDragOver: (e: React.DragEvent) => {
        if (dragSkill) {
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
          setDropTarget(targetId);
        }
      },
      onDragLeave: () => setDropTarget((t) => (t === targetId ? null : t)),
      onDrop: (e: React.DragEvent) => {
        e.preventDefault();
        const sid = e.dataTransfer.getData('text/skillsmcp-skill') || dragSkill;
        setDropTarget(null);
        setDragSkill(null);
        if (sid) {
          if (isProject) void moveSkill(sid, 'project', targetId);
          else void moveSkill(sid, 'global', '');
        }
      },
    };
  }

  function sideRow(s: Skill) {
    return (
      <div key={s.id} draggable
        onDragStart={(e) => {
          e.dataTransfer.setData('text/skillsmcp-skill', s.id);
          e.dataTransfer.effectAllowed = 'move';
          setDragSkill(s.id);
        }}
        onDragEnd={() => { setDragSkill(null); setDropTarget(null); }}
        className={cn(dragSkill === s.id && 'opacity-40')}>
        <button onClick={() => goSkill(s.id)}
          className={cn('group flex w-full cursor-grab select-none items-center gap-2 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-zinc-800 active:cursor-grabbing',
            activeId === s.id && 'acc-bar bg-zinc-800')}>
          <span className={cn('h-2 w-2 shrink-0 rounded-full', s.enabled ? 'bg-emerald-400' : 'bg-zinc-600')} />
          <span className="min-w-0 flex-1">
            <span className="block truncate font-mono text-[12px] text-zinc-100">{s.name}</span>
            <span className="block truncate text-[11px] text-zinc-500">{s.description}</span>
          </span>
        </button>
      </div>
    );
  }

  async function createProjectInline() {
    const name = newProjectName.trim();
    if (!name) return;
    try {
      const slug = String(await api.NormalizeSlug(name));
      await api.CreateProject({ name, slug, description: '', color: '' });
      setNewProjectName('');
      setShowNewProject(false);
      await refresh();
    } catch (e: any) {
      setErr(e?.message || String(e));
    }
  }

  const TABS: { id: Tab; label: string; icon: any }[] = [
    { id: 'home', label: 'Home', icon: LayoutGrid },
    { id: 'skills', label: 'Skill', icon: BookOpen },
    { id: 'projects', label: 'Projects', icon: FolderKanban },
    { id: 'mcp', label: 'MCP', icon: Plug2 },
    { id: 'logs', label: 'Logs', icon: ScrollText },
  ];

  return (
    <div className="flex h-full flex-col bg-zinc-950 text-zinc-200">
      <Menubar tab={tab} onTab={setTab} onAdd={() => openNewSkill()} onCopyMCP={copyMCP}
        onOpenSettings={() => setSettingsOpen(true)} onToggleSidebar={() => setSideOpen((v) => !v)} />
      {updateBanner && updateInfo?.updateAvailable && (
        <UpdateBanner info={updateInfo} busy={updateBusy} progress={updateProgress}
          onInstall={installUpdate} onNotes={openReleasePage}
          onLater={() => setUpdateBanner(false)} onSkip={skipUpdateVersion} />
      )}

      <div className="flex min-h-0 flex-1">
        {sideOpen && (
          <>
            <aside className="glassbar flex min-h-0 shrink-0 flex-col border-r border-zinc-800 bg-zinc-900/60" style={{ width: sideW }}>
              <div className="px-3 pb-2 pt-3">
                <div className="flex items-center gap-2">
                  <img src="/favicon.png" alt="SkillsMCP" className="h-8 w-8 shrink-0 object-contain" />
                  <div className="min-w-0 flex-1 truncate text-[13px] font-semibold tracking-tight text-zinc-100">SkillsMCP</div>
                  <Button variant="emerald" title="New skill" onClick={() => openNewSkill()} className="!rounded-lg !px-2 !py-2"><Plus size={15} /></Button>
                </div>
                <div className="mt-1.5 flex items-center gap-2">
                  <div className="min-w-0 flex-1 truncate font-mono text-[10px] text-zinc-500">{enabledCount}/{skills.length} enabled · {globals.filter((s) => s.enabled).length} global</div>
                </div>
              </div>

              <div className="px-3 pb-2">
                <div className="relative">
                  <Search size={13} className="absolute left-2.5 top-2.5 text-zinc-600" />
                  <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search skills…" className="!pl-8" />
                </div>
              </div>

              <div className="flex-1 overflow-y-auto px-2 pb-2">
                {/* GLOBAL section — drop target for the main MCP */}
                <div {...dropProps('global', false)}
                  className={cn('mb-1 rounded-lg', dropTarget === 'global' && 'bg-emerald-500/10 outline outline-1 outline-emerald-500/40')}>
                  <div className="flex items-center gap-1.5 px-2 pb-0.5 pt-2">
                    <Globe size={11} className="shrink-0 text-emerald-400/80" />
                    <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">Global · main MCP</span>
                    <span className="ml-auto font-mono text-[10px] text-zinc-600">
                      {globals.filter((s) => s.enabled).length}/{globals.length}{dropTarget === 'global' ? ' · drop here' : ''}
                    </span>
                  </div>
                  {globals.filter(matchesQuery).map(sideRow)}
                </div>
                {/* PROJECT sections — ReadGate-cluster style, each a drop target */}
                {projects.map((p) => {
                  const list = skills.filter((s) => s.projectId === p.id && matchesQuery(s));
                  const live = skills.filter((s) => s.projectId === p.id && s.enabled).length;
                  const isOpen = expanded[p.id] !== false;
                  const isTarget = dropTarget === p.id;
                  return (
                    <div key={p.id} {...dropProps(p.id, true)}
                      className={cn('mb-1 rounded-lg', isTarget && 'bg-emerald-500/10 outline outline-1 outline-emerald-500/40')}>
                      <div className="flex items-center gap-1 rounded-md px-1.5 pb-0.5 pt-2">
                        <button onClick={() => setExpanded((e) => ({ ...e, [p.id]: !isOpen }))}
                          title={isOpen ? 'Collapse' : 'Expand'}
                          className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200">
                          {isOpen ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                        </button>
                        <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: p.color || '#52525b' }} />
                        <button onClick={() => setTab('projects')} title={`Open ${p.name}`}
                          className="min-w-0 flex-1 truncate text-left text-[10px] font-semibold uppercase tracking-wider text-zinc-500 hover:text-zinc-300">
                          {p.slug}
                        </button>
                        <span className="font-mono text-[10px] text-zinc-600">
                          {live}/{list.length}{isTarget ? ' · drop here' : ''}
                        </span>
                      </div>
                      {isOpen && <div className="ml-4 border-l border-zinc-800 pl-1">{list.map(sideRow)}</div>}
                    </div>
                  );
                })}
                {globals.filter(matchesQuery).length === 0 && skills.filter(matchesQuery).length === 0 && (
                  <div className="p-3 text-center text-[11px] text-zinc-600">
                    {skills.length === 0 ? 'No skills yet — create your first.' : `No match for "${query}".`}
                  </div>
                )}

                <button onClick={() => setShowNewProject(true)} className="mt-2 flex w-full items-center gap-2 rounded-lg border border-dashed border-zinc-800 px-3 py-2 text-xs text-zinc-500 hover:border-zinc-700 hover:text-zinc-300">
                  <FolderPlus size={13} /> New project — own MCP
                </button>
                {showNewProject && (
                  <div className="mt-2 rounded-lg border border-zinc-800 bg-zinc-950 p-2.5">
                    <Input value={newProjectName} onChange={(e) => setNewProjectName(e.target.value)} placeholder="e.g. my-app"
                      onKeyDown={(e) => e.key === 'Enter' && createProjectInline()} />
                    <div className="mt-2 flex gap-1.5">
                      <Button variant="emerald" className="!py-1.5 text-xs" onClick={createProjectInline}>Create</Button>
                      <Button variant="ghost" className="!py-1.5 text-xs" onClick={() => setShowNewProject(false)}>Cancel</Button>
                    </div>
                  </div>
                )}
              </div>

              <div className="border-t border-zinc-800 p-3">
                <div className="flex items-center gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 px-3 py-2">
                  <span className="relative flex h-2 w-2"><span className="absolute h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" /><span className="h-2 w-2 rounded-full bg-emerald-400" /></span>
                  <span className="truncate font-mono text-[11px] text-emerald-200/90">mcp · {mcpUrl.replace('http://', '')}</span>
                  <button onClick={() => refresh()} className="ml-auto shrink-0 text-zinc-500 hover:text-zinc-200"><RefreshCw size={13} /></button>
                </div>
                <div className="mt-1.5 truncate font-mono text-[10px] text-zinc-600" title={preview}>{preview || 'loading tools…'}</div>
              </div>
            </aside>
            <div className="flex items-stretch py-2">
              <div onMouseDown={startSide} title="Drag to resize sidebar"
                className="w-1.5 shrink-0 cursor-col-resize self-stretch rounded-full bg-transparent transition-colors hover:bg-zinc-700" />
            </div>
          </>
        )}

        <main className="flex min-w-0 flex-1 flex-col">
          <header className="flex flex-wrap items-center gap-x-2 gap-y-1.5 border-b border-zinc-800 bg-zinc-950/80 px-3 py-2 backdrop-blur">
            <div className="flex items-center gap-0.5 overflow-x-auto rounded-lg border border-zinc-800 bg-zinc-900 p-0.5">
              {TABS.map((t) => (
                <button key={t.id} onClick={() => setTab(t.id)}
                  className={cn('flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1 text-xs text-zinc-400 hover:text-zinc-100', tab === t.id && 'bg-zinc-700/60 text-white shadow')}>
                  <t.icon size={13} /> <span className="hidden min-[1100px]:inline">{t.label}</span>
                </button>
              ))}
            </div>
            <div className="ms-auto flex items-center gap-1.5">
              {(tab === 'home' || tab === 'skills') && (
                <>
                  <Button variant="outline" className="!py-1.5 text-[11px]" onClick={() => setImportSheet({ open: true, scope: 'global', projectId: '' })} title="Import .md files as skills — global or into a project">
                    <Upload size={12} /> <span className="hidden min-[1100px]:inline">Import</span>
                  </Button>
                  <Button variant="outline" className="!py-1.5 text-[11px]" onClick={copyMCP}>
                    {copied ? <Check size={12} /> : <Copy size={12} />} {copied ? 'Copied' : 'MCP config'}
                  </Button>
                  <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={() => openNewSkill()}><Plus size={12} /> New Skill</Button>
                </>
              )}
              {tab === 'projects' && (
                <Button variant="emerald" className="!py-1.5 text-[11px]"
                  onClick={() => setProjectSheet({ open: true, editing: null, initial: { ...EMPTY_PROJECT } })}>
                  <Plus size={12} /> New Project
                </Button>
              )}
            </div>
          </header>

          <div className="grid-paper flex-1 overflow-y-auto p-3">
            {err && (
              <div className="mx-auto mb-3 grid max-w-6xl rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-300">{err}</div>
            )}

            {/* ---------- HOME: all skills as cards ---------- */}
            {tab === 'home' && (
              <div className="mx-auto grid max-w-6xl gap-3">
                <Card className="flex flex-wrap items-center gap-3 p-4">
                  <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-500/15 text-emerald-300"><LayoutGrid size={18} /></div>
                  <div className="min-w-0 flex-1">
                    <div className="text-[15px] font-semibold text-white">All skills — global live on the main MCP, project skills on their own MCP</div>
                    <div className="mt-0.5 text-xs text-zinc-500">
                      <span className="text-emerald-300">Global</span> = <code className="font-mono">SkillsMCP mcp</code> ·
                      {' '}<span className="text-indigo-300">Project</span> = <code className="font-mono">SkillsMCP mcp --project &lt;slug&gt;</code>
                    </div>
                  </div>
                  <div className="flex gap-1.5">
                    <Button variant="outline" className="!py-1.5 text-[11px]" onClick={() => openNewSkill('global')}><Globe size={12} /> Global skill</Button>
                    <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={() => openNewSkill()}><Plus size={12} /> New Skill</Button>
                  </div>
                </Card>

                <div className="flex flex-wrap items-center gap-1.5">
                  {([
                    { v: 'all', label: `All · ${skills.length}` },
                    { v: 'global', label: `Global · ${globals.length}` },
                  ] as { v: HomeFilter; label: string }[]).map((f) => (
                    <button key={f.v} onClick={() => setHomeFilter(f.v)}
                      className={cn('rounded-lg border px-2.5 py-1.5 text-[11px] transition',
                        homeFilter === f.v ? 'acc-border acc-soft acc-text border' : 'border-zinc-800 text-zinc-400 hover:bg-zinc-800')}>
                      {f.label}
                    </button>
                  ))}
                  {projects.map((p) => {
                    const v = `project:${p.id}`;
                    const n = skills.filter((s) => s.projectId === p.id).length;
                    return (
                      <button key={p.id} onClick={() => setHomeFilter(homeFilter === v ? 'all' : v)}
                        className={cn('flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[11px] transition',
                          homeFilter === v ? 'acc-border acc-soft acc-text border' : 'border-zinc-800 text-zinc-400 hover:bg-zinc-800')}>
                        <span className="h-2 w-2 rounded-full" style={{ background: p.color }} />
                        {p.slug} · {n}
                      </button>
                    );
                  })}
                </div>

                {filtered.length === 0 ? (
                  <Empty icon={<BookOpen size={22} />} title="No skills here yet"
                    hint="Create a global skill (main MCP) or a project skill (that project's own MCP) with New Skill." />
                ) : homeFilter === 'all' ? (
                  <div className="grid gap-5">
                    {(() => {
                      const g = filtered.filter((s) => s.scope !== 'project');
                      return g.length > 0 && (
                        <section>
                          <div className="mb-1.5 flex items-center gap-1.5 px-0.5">
                            <span className="h-2 w-2 rounded-full bg-emerald-400" />
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-400">Global</span>
                            <span className="font-mono text-[10px] text-zinc-600">{g.length} · main MCP</span>
                          </div>
                          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                            {g.map(skillCard)}
                          </div>
                        </section>
                      );
                    })()}
                    {projects.map((p) => {
                      const list = filtered.filter((s) => s.projectId === p.id);
                      if (list.length === 0) return null;
                      return (
                        <section key={p.id}>
                          <button onClick={() => setHomeFilter(`project:${p.id}`)} title={`Show only ${p.slug}`}
                            className="mb-1.5 flex items-center gap-1.5 rounded-md px-0.5 text-left hover:opacity-80">
                            <span className="h-2 w-2 rounded-full" style={{ background: p.color || '#6366f1' }} />
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-400">{p.slug}</span>
                            <span className="font-mono text-[10px] text-zinc-600">{list.length} · project MCP</span>
                          </button>
                          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                            {list.map(skillCard)}
                          </div>
                        </section>
                      );
                    })}
                  </div>
                ) : (
                  <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                    {filtered.map(skillCard)}
                  </div>
                )}
              </div>
            )}

            {/* ---------- SKILL tab: preview + live edit of the focused skill ---------- */}
            {tab === 'skills' && (
              !active ? (
                <div className="mx-auto grid max-w-5xl gap-3">
                  <Empty icon={<BookOpen size={22} />} title="No skill selected"
                    hint="Click any skill — here, in the sidebar, or in Projects — to preview it and edit it live." />
                  <div className="flex justify-center">
                    <Button variant="emerald" onClick={() => openNewSkill()}><Plus size={13} /> New skill (sheet)</Button>
                  </div>
                </div>
              ) : (
                <SkillDetail
                  key={active.id}
                  skill={active}
                  onChanged={(s) => {
                    setSkills((prev) => prev.map((x) => (x.id === s.id ? s : x)));
                    api.MCPToolsPreview().then((v) => setPreview(String(v))).catch(() => {});
                  }}
                  onToggle={toggleEnabled}
                  onDelete={(s) => setConfirm({
                    title: `Delete skill "${s.name}"?`,
                    body: `Its MCP tool disappears on the next tools/list. This cannot be undone.`,
                    confirmLabel: 'Delete skill',
                    action: async () => {
                      await api.DeleteSkill(s.id);
                      if (activeId === s.id) setActiveId('');
                      await refresh();
                    },
                  })}
                  onExport={exportSkill}
                  onOpenSheet={openSkillSheet}
                />
              )
            )}

            {/* ---------- PROJECTS ---------- */}
            {tab === 'projects' && (
              <div className="mx-auto grid max-w-6xl gap-3">
                <Card className="flex flex-wrap items-center gap-3 p-4">
                  <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-500/15 text-indigo-300"><FolderKanban size={18} /></div>
                  <div className="min-w-0 flex-1">
                    <div className="text-[15px] font-semibold text-white">Projects — each gets its own MCP</div>
                    <div className="mt-0.5 text-xs text-zinc-500">
                      Project MCP = <code className="font-mono text-zinc-300">SkillsMCP mcp --project &lt;slug&gt;</code> → globals + that project's skills.
                    </div>
                  </div>
                  <Button variant="emerald" className="!py-1.5 text-[11px]"
                    onClick={() => setProjectSheet({ open: true, editing: null, initial: { ...EMPTY_PROJECT } })}>
                    <Plus size={12} /> New Project
                  </Button>
                </Card>

                {projects.length === 0 ? (
                  <Empty icon={<FolderKanban size={22} />} title="No projects yet"
                    hint="Create a project, then add project-scoped skills to it. Its MCP serves globals + its own skills." />
                ) : (
                  <div className="grid gap-3 md:grid-cols-2">
                    {projects.map((p) => {
                      const pskills = skills.filter((s) => s.projectId === p.id);
                      const live = pskills.filter((s) => s.enabled).length;
                      return (
                        <Card key={p.id} className="overflow-hidden">
                          <div className="h-1" style={{ background: p.color }} />
                          <div className="p-3">
                            <div className="flex items-center gap-2">
                              <span className="text-[13px] font-semibold text-zinc-100">{p.name}</span>
                              <code className="truncate font-mono text-[11px] text-emerald-300">skillsmcp-{p.slug}</code>
                              <span className="ml-auto"><Badge tone={live > 0 ? 'green' : 'zinc'}>{live}/{pskills.length} live</Badge></span>
                            </div>
                            {p.description && <div className="mt-0.5 text-[11px] text-zinc-500">{p.description}</div>}
                            <div className="mt-2 grid gap-1">
                              {pskills.slice(0, 5).map((s) => (
                                <button key={s.id} onClick={() => goSkill(s.id)}
                                  className="flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-left text-[11px] hover:border-zinc-600">
                                  <span className={cn('h-1.5 w-1.5 rounded-full', s.enabled ? 'bg-emerald-400' : 'bg-zinc-600')} />
                                  <code className="truncate font-mono text-zinc-200">{s.name}</code>
                                  <span className="ml-auto truncate text-zinc-500">{s.description.slice(0, 40)}</span>
                                </button>
                              ))}
                              {pskills.length === 0 && (
                                <div className="rounded-md border border-dashed border-zinc-800 px-2 py-2 text-center text-[11px] text-zinc-600">
                                  No skills yet — New Skill → Project → {p.slug}.
                                </div>
                              )}
                              {pskills.length > 5 && (
                                <button onClick={() => { setHomeFilter(`project:${p.id}`); setTab('home'); }} className="text-center font-mono text-[10px] text-zinc-500 hover:text-zinc-200">
                                  +{pskills.length - 5} more → view in Home
                                </button>
                              )}
                            </div>
                            <div className="mt-2.5 flex flex-wrap gap-1.5">
                              <Button variant="emerald" className="!py-1 text-[11px]" onClick={() => openNewSkill('project', p.id)}>
                                <Plus size={11} /> Skill here
                              </Button>
                              <Button variant="outline" className="!py-1 text-[11px]" title={`Import .md files into ${p.slug}`}
                                onClick={() => setImportSheet({ open: true, scope: 'project', projectId: p.id })}>
                                <Upload size={11} /> Import
                              </Button>
                              <Button variant="outline" className="!py-1 text-[11px]"
                                onClick={async () => { const c = await loadProjCfg(p.slug); navigator.clipboard.writeText(c); }}>
                                <Copy size={11} /> MCP config
                              </Button>
                              <Button variant="ghost" className="!py-1 text-[11px]"
                                onClick={() => setProjectSheet({ open: true, editing: p, initial: { name: p.name, slug: p.slug, description: p.description, color: p.color } })}>
                                <Pencil size={11} /> Edit
                              </Button>
                              <IconBtn title={`Delete ${p.slug}`} tip="Delete project + its skills" size="sm" className="ml-auto hover:!text-red-300"
                                onClick={() => setConfirm({
                                  title: `Delete project "${p.name}"?`,
                                  body: `${pskills.length} project skill(s) will be DELETED with it. Globals are kept. Its MCP skillsmcp-${p.slug} stops working. This cannot be undone.`,
                                  confirmLabel: 'Delete project',
                                  action: async () => { await api.DeleteProject(p.id); await refresh(); },
                                })}>
                                <Trash2 size={13} />
                              </IconBtn>
                            </div>
                            <div className="mt-2 flex items-start gap-1.5 rounded-lg border border-zinc-800 bg-zinc-950 p-2 font-mono text-[10px] text-zinc-500">
                              <Globe size={11} className="mt-0.5 shrink-0" />
                              <span>SkillsMCP mcp --project {p.slug}</span>
                              <button className="ml-auto shrink-0 underline hover:text-zinc-200"
                                onClick={async () => {
                                  setProjTest((t) => ({ ...t, [p.slug]: 'testing…' }));
                                  try {
                                    const out = String(await api.TestProjectMCP(p.slug));
                                    setProjTest((t) => ({ ...t, [p.slug]: out }));
                                  } catch (e: any) {
                                    const msg = 'failed: ' + (e?.message || String(e));
                                    setProjTest((t) => ({ ...t, [p.slug]: msg }));
                                  }
                                }}>
                                <span className="flex items-center gap-1"><FlaskConical size={10} /> test</span>
                              </button>
                            </div>
                            {projTest[p.slug] && (
                              <pre className="mt-1.5 max-h-32 overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2 font-mono text-[10px] text-zinc-300">{projTest[p.slug]}</pre>
                            )}
                          </div>
                        </Card>
                      );
                    })}
                  </div>
                )}
              </div>
            )}

            {tab === 'mcp' && <McpPanel skills={skills} projects={projects} mcpUrl={mcpUrl} />}

            {tab === 'logs' && (
              <div className="mx-auto grid max-w-5xl gap-3">
                <Card className="p-4">
                  <div className="mb-2 flex items-center gap-2">
                    <ScrollText size={14} className="text-zinc-400" />
                    <span className="text-[13px] font-semibold text-zinc-100">Diagnostic log</span>
                    <Button variant="outline" className="ml-auto !py-1 text-[11px]" onClick={reloadLogs}>Refresh</Button>
                    <Button variant="ghost" className="!py-1 text-[11px]" onClick={() => { api.ClearLogs().catch(() => {}); setLogs([]); }}>Clear</Button>
                  </div>
                  <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded-lg codeblock bg-black/60 p-2.5 font-mono text-[11px] text-zinc-300">
                    {logs.length ? logs.join('\n') : 'no log yet'}
                  </pre>
                  <div className="mt-2 font-mono text-[10px] text-zinc-600">
                    store: {storePath || 'loading…'} · SkillsMCP {version}
                  </div>
                </Card>
              </div>
            )}
          </div>
        </main>
      </div>

      <SettingsSheet open={settingsOpen} onClose={() => setSettingsOpen(false)}
        appearance={appearance} onPatch={patchAppearance} storePath={storePath}
        counts={{ skills: skills.length, projects: projects.length, enabled: skills.filter((s) => s.enabled).length }}
        mcpUrl={mcpUrl} mcpConfig={mcpConfig} logs={logs} logPath={logPath}
        onReloadLogs={reloadLogs} onClearLogs={clearLogs} version={version}
        updateInfo={updateInfo} updateChecking={updateChecking} updateBusy={updateBusy}
        updateMsg={updateMsg} onCheckUpdates={() => runUpdateCheck(true)}
        onInstallUpdate={installUpdate} onOpenRelease={openReleasePage} />

      <SkillSheet open={skillSheet.open} onClose={() => setSkillSheet((s) => ({ ...s, open: false }))}
        initial={skillSheet.initial} editing={skillSheet.editing} projects={projects}
        onSaved={(s) => { setActiveId(s.id); refresh(s.id); }} />

      <ProjectSheet open={projectSheet.open} onClose={() => setProjectSheet((s) => ({ ...s, open: false }))}
        initial={projectSheet.initial} editing={projectSheet.editing}
        onSaved={() => refresh()} />

      <ImportSheet open={importSheet.open} onClose={() => setImportSheet((s) => ({ ...s, open: false }))}
        initialScope={importSheet.scope} initialProjectId={importSheet.projectId}
        projects={projects} onImported={() => refresh()} />

      <ConfirmModal open={!!confirm} title={confirm?.title || ''} body={confirm?.body || ''}
        confirmLabel={confirm?.confirmLabel || 'Confirm'} busy={confirmBusy}
        onCancel={() => { if (!confirmBusy) setConfirm(null); }}
        onConfirm={async () => {
          if (!confirm) return;
          setConfirmBusy(true);
          try { await confirm.action(); } catch (e: any) { setErr(e?.message || String(e)); }
          finally { setConfirmBusy(false); setConfirm(null); }
        }}       />
    </div>
  );
}
