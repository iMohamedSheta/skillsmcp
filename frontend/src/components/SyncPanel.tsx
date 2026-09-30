import { useEffect, useState } from 'react';
import {
  Github, ArrowUpToLine, ArrowDownToLine, RefreshCw, Loader2,
  Check, Plus, Eye, EyeOff, ShieldCheck, KeyRound,
  FolderOpen, PencilLine, Copy,
} from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill, Workspace } from '../lib/types';
import { sanitizeRemote } from '../lib/sanitize';
import { Badge, Button, Card, Input } from './ui';
import { cn } from '../lib/cn';

// Sync tab: git remote on ANY host + push/pull, clone.
// Each workspace has its own remote/branch/checkout — the provider
// doesn't matter (GitHub, GitLab, Bitbucket, Codeberg, self-hosted,
// local path; SSH or HTTPS). Auth reuses the machine's git setup
// (SSH keys/agent, credential manager, gh auth) — no token needed when
// git itself is authenticated. An optional HTTPS token covers machines
// where git isn't set up yet. Conflicts are resolved in the editor:
// open the checkout folder and merge by hand, then push.
export default function SyncPanel({ workspace, skills, projects, onChanged, onError }: {
  workspace: Workspace;
  skills: Skill[];
  projects: Project[];
  onChanged: (switchToId?: string) => void;
  onError: (msg: string) => void;
}) {
  const [git, setGit] = useState({ remote: workspace.gitRemote || '', branch: workspace.gitBranch || 'main', token: '' });
  const [gitSaving, setGitSaving] = useState(false);
  const [gitBusy, setGitBusy] = useState<'push' | 'pull' | 'reset' | null>(null);
  const [gitOut, setGitOut] = useState('');
  const [confirmForce, setConfirmForce] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const [showAuth, setShowAuth] = useState(false);
  const [showRemote, setShowRemote] = useState(false);
  const [status, setStatus] = useState('loading…');
  const [clone, setClone] = useState({ name: '', remote: '', branch: 'main', token: '' });
  const [cloneBusy, setCloneBusy] = useState(false);
  const [showCloneAuth, setShowCloneAuth] = useState(false);
  const [checkoutDir, setCheckoutDir] = useState('');
  const [checkoutCopied, setCheckoutCopied] = useState(false);

  useEffect(() => {
    setGit({ remote: workspace.gitRemote || '', branch: workspace.gitBranch || 'main', token: '' });
    setGitOut('');
    setConfirmForce(false);
    setConfirmReset(false);
    setShowAuth(false);
    setShowRemote(false);
    reloadStatus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace.id]);

  async function reloadStatus() {
    try {
      setStatus(String(await (api as any).WorkspaceGitStatus(workspace.id)));
    } catch {
      setStatus('status unavailable');
    }
    try {
      setCheckoutDir(String(await (api as any).WorkspaceCheckoutDir(workspace.id)));
    } catch {
      setCheckoutDir('');
    }
  }

  async function openFolder() {
    try {
      await (api as any).OpenCheckoutFolder(workspace.id);
    } catch (e: any) {
      onError(e?.message || String(e));
    }
  }

  async function openEditor() {
    try {
      await (api as any).OpenCheckoutEditor(workspace.id);
    } catch (e: any) {
      onError(e?.message || String(e));
    }
  }

  async function copyCheckoutDir() {
    if (!checkoutDir) return;
    try {
      await navigator.clipboard.writeText(checkoutDir);
      setCheckoutCopied(true);
      setTimeout(() => setCheckoutCopied(false), 1400);
    } catch (e: any) {
      onError(e?.message || String(e));
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

  function failOut(r: any, fallback: string) {
    const parts = ['failed: ' + (r?.error || fallback)];
    if (r?.detail) parts.push('remote says:\n' + r.detail);
    if (r?.hint) parts.push('fix: ' + r.hint);
    return parts.join('\n\n');
  }

  async function doPush(force = false) {
    setGitBusy('push');
    setGitOut('');
    setConfirmForce(false);
    try {
      const r = (await (api as any).PushWorkspace(workspace.id, force)) as any;
      setGitOut(r?.ok ? `✓ ${r.detail || 'pushed'}` : failOut(r, 'unknown'));
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
      setGitOut(r?.ok ? `✓ ${r.detail || 'pulled'}` : failOut(r, 'unknown'));
      onChanged();
      reloadStatus();
    } catch (e: any) {
      setGitOut('failed: ' + (e?.message || String(e)));
    } finally {
      setGitBusy(null);
    }
  }

  async function doReset() {
    setGitBusy('reset');
    setGitOut('');
    setConfirmReset(false);
    try {
      const r = (await (api as any).ResetWorkspace(workspace.id)) as any;
      setGitOut(r?.ok ? `✓ ${r.detail || 'reset to remote'}` : failOut(r, 'unknown'));
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

  const linked = sanitizeRemote(workspace.gitRemote || '');
  const draft = sanitizeRemote(git.remote);
  const globals = skills.filter((s) => s.scope !== 'project');

  return (
    <div className="mx-auto grid max-w-6xl gap-3">
      {/* header / sync preview */}
      <Card className="flex flex-wrap items-center gap-3 p-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-500/15 text-emerald-300">
          <Github size={18} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[15px] font-semibold text-white">Sync — {workspace.name}</span>
            {workspace.gitRemote
              ? <Badge tone="green">linked</Badge>
              : <Badge tone="zinc">local only</Badge>}
            {workspace.hasToken && <Badge tone="indigo">token saved</Badge>}
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
            {globals.filter((s) => s.enabled).length}/{globals.length} global · {projects.length} project(s) · {skills.length} skill(s) in this workspace
          </div>
        </div>
        <button onClick={reloadStatus} className="ml-auto flex items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-200">
          <RefreshCw size={11} /> status
        </button>
      </Card>

      <div className="rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-2 font-mono text-[11px] text-zinc-400">{status}</div>

      {/* local checkout — fix it by hand when the UI fix isn't enough */}
      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <FolderOpen size={14} className="text-zinc-400" /> Local checkout — fix it by hand
        </div>
        <div className="mb-2 text-[11px] leading-relaxed text-zinc-500">
          Diverged history or a rejected push is resolved in the checkout itself — open it in your editor,
          merge the conflicted files by hand, then push again with the buttons below.
        </div>
        {checkoutDir && (
          <div className="mb-2 flex flex-wrap items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-2">
            <code className="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-200" title={checkoutDir}>{checkoutDir}</code>
            <button onClick={copyCheckoutDir} className="flex shrink-0 items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-200" title="Copy checkout path">
              {checkoutCopied ? <Check size={11} /> : <Copy size={11} />} {checkoutCopied ? 'copied' : 'copy path'}
            </button>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-1.5">
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={openFolder} title="Reveal the checkout in the file manager">
            <FolderOpen size={12} /> Open folder
          </Button>
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={openEditor} title="Open the checkout in the editor (VS Code when available)">
            <PencilLine size={12} /> Editor
          </Button>
        </div>
      </Card>

      {/* link a remote on any host */}
      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <Github size={14} className="text-zinc-400" /> Git remote — any host, SSH or HTTPS
        </div>
        <div className="mb-2.5 text-[11px] leading-relaxed text-zinc-500">
          Manual sync, nothing pushes itself. Works with any provider (GitHub, GitLab, Bitbucket, Codeberg, self-hosted)
          or a local path. SSH remotes (<code className="font-mono">git@host:org/repo.git</code>) use your keys/agent —
          no token needed. HTTPS works with your system's credential helper when git on this machine is already
          authenticated — again no token needed. A token is only for private HTTPS repos on machines where git itself
          isn't authenticated yet.
          Layout on disk: <code className="font-mono">workspace.json</code> + <code className="font-mono">globals/</code> + <code className="font-mono">projects/&lt;slug&gt;/</code> (same lossless format as Export).
        </div>
        {workspace.gitRemote && (
          <div className="mb-2 flex flex-wrap items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-2">
            <ShieldCheck size={12} className="shrink-0 text-emerald-400" />
            <code className="min-w-0 flex-1 truncate font-mono text-[11px] text-zinc-200">
              {showRemote ? workspace.gitRemote : (linked.display || '(empty)')}
            </code>
            {linked.hasCreds && <Badge tone="amber">credentials embedded — consider removing</Badge>}
            <button onClick={() => setShowRemote((v) => !v)} className="flex shrink-0 items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-200">
              {showRemote ? <EyeOff size={11} /> : <Eye size={11} />} {showRemote ? 'hide' : 'show full URL'}
            </button>
          </div>
        )}
        <div className="grid gap-2 md:grid-cols-[1fr_160px]">
          <Input value={git.remote} onChange={(e) => setGit((g) => ({ ...g, remote: e.target.value }))} placeholder="git@host:org/skills.git  or  https://host/org/skills.git  (empty = local only)" spellCheck={false} className="font-mono !text-[12px]" />
          <Input value={git.branch} onChange={(e) => setGit((g) => ({ ...g, branch: e.target.value }))} placeholder="main" spellCheck={false} className="font-mono !text-[12px]" />
        </div>
        {draft.hasCreds && git.remote.trim() && (
          <div className="mt-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-2 text-[11px] text-amber-200">
            This URL embeds credentials (<code className="font-mono">{draft.display}</code>). Passwords in URLs leak into logs and
            error output — prefer linking the clean URL and letting system git auth (or the optional token below) handle
            authentication.
          </div>
        )}
        <div className="mt-2">
          <button onClick={() => setShowAuth((v) => !v)} className="flex items-center gap-1.5 text-[11px] text-zinc-400 hover:text-zinc-200">
            <KeyRound size={11} /> {showAuth ? 'Hide authentication (optional)' : 'Authentication — optional HTTPS token…'}
          </button>
          {showAuth && (
            <div className="mt-2 grid gap-2 rounded-lg border border-zinc-800 bg-zinc-950 p-2.5">
              <div className="text-[11px] leading-relaxed text-zinc-500">
                Skip this when git on this machine is already authenticated (SSH keys/agent, Git Credential Manager,
                <code className="font-mono"> gh auth</code>). Only paste a token for a private HTTPS repo on a machine where git
                itself isn't set up. Stored locally, never shown again, never written into the repo, sent per-command as a
                header — never embedded in the URL.
              </div>
              <div className="relative">
                <Input value={git.token} onChange={(e) => setGit((g) => ({ ...g, token: e.target.value }))} placeholder={workspace.hasToken ? '•••••• token saved — leave blank to keep' : 'Token for private HTTPS repos (optional)'} spellCheck={false} type="password" className="font-mono !text-[12px]" />
              </div>
              <div className="flex items-center gap-1.5">
                {workspace.hasToken && <Badge tone="green">token saved</Badge>}
              </div>
            </div>
          )}
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={saveGit} disabled={gitSaving}>
            {gitSaving ? <Loader2 size={12} className="animate-spin" /> : <Check size={12} />} Link repo
          </Button>
          <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={() => doPush(false)} disabled={gitBusy !== null || !workspace.gitRemote}>
            {gitBusy === 'push' ? <Loader2 size={12} className="animate-spin" /> : <ArrowUpToLine size={12} />} Push
          </Button>
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={() => doPull()} disabled={gitBusy !== null || !workspace.gitRemote}>
            {gitBusy === 'pull' ? <Loader2 size={12} className="animate-spin" /> : <ArrowDownToLine size={12} />} Pull
          </Button>
          {!confirmForce ? (
            <Button variant="ghost" className="!py-1.5 text-[11px] hover:!text-amber-300" onClick={() => setConfirmForce(true)} disabled={gitBusy !== null || !workspace.gitRemote} title="Overwrite the repo with your library">
              Force push…
            </Button>
          ) : (
            <span className="flex items-center gap-1.5 text-[11px] text-amber-200">
              Overwrite the repo?
              <Button variant="outline" className="!py-1 text-[11px] hover:!text-amber-300" onClick={() => doPush(true)}>Confirm</Button>
              <Button variant="ghost" className="!py-1 text-[11px]" onClick={() => setConfirmForce(false)}>Cancel</Button>
            </span>
          )}
          {!confirmReset ? (
            <Button variant="ghost" className="!py-1.5 text-[11px] hover:!text-amber-300" onClick={() => setConfirmReset(true)} disabled={gitBusy !== null || !workspace.gitRemote} title="Discard checkout, take the repo exactly, restore additively">
              Reset to repo…
            </Button>
          ) : (
            <span className="flex items-center gap-1.5 text-[11px] text-amber-200">
              Take the repo's side? Local-only skills stay.
              <Button variant="outline" className="!py-1 text-[11px] hover:!text-amber-300" onClick={doReset}>Confirm</Button>
              <Button variant="ghost" className="!py-1 text-[11px]" onClick={() => setConfirmReset(false)}>Cancel</Button>
            </span>
          )}
        </div>
        {gitOut && <div className="mt-1.5 whitespace-pre-wrap font-mono text-[11px] text-zinc-300">{gitOut}</div>}
        {gitOut && /force|rejected|fetch first|diverg|non-fast-forward/i.test(gitOut) && (
          <div className="mt-1.5 flex flex-wrap items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-2 text-[11px] text-amber-200">
            <span>Push rejected — the repo has commits you don't have. Open the checkout in your editor, merge by hand, then push again (or force-push to overwrite the repo).</span>
            <Button variant="emerald" className="!py-1 text-[11px]" onClick={openEditor}>
              <PencilLine size={11} /> Open in editor
            </Button>
          </div>
        )}
      </Card>

      {/* connect a repo as a new workspace */}
      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100">
          <Plus size={14} className="text-emerald-300" /> Connect a repo as a new workspace
        </div>
        <div className="mb-2 text-[11px] text-zinc-500">
          Clones a public or private repo on any host and imports every skill. Private repos work with your existing git
          auth (SSH/agent, credential manager, <code className="font-mono">gh auth</code>) — a token is only needed where git
          itself isn't authenticated. Each project inside keeps its own MCP.
        </div>
        <div className="grid gap-2 md:grid-cols-[200px_1fr_140px]">
          <Input value={clone.name} onChange={(e) => setClone((c) => ({ ...c, name: e.target.value }))} placeholder="Workspace name" />
          <Input value={clone.remote} onChange={(e) => setClone((c) => ({ ...c, remote: e.target.value }))} placeholder="git@host:org/skills.git  or  https://host/org/skills.git" spellCheck={false} className="font-mono !text-[12px]" />
          <Input value={clone.branch} onChange={(e) => setClone((c) => ({ ...c, branch: e.target.value }))} placeholder="main" spellCheck={false} className="font-mono !text-[12px]" />
        </div>
        <div className="mt-2">
          <button onClick={() => setShowCloneAuth((v) => !v)} className={cn('flex items-center gap-1.5 text-[11px] text-zinc-500 hover:text-zinc-200')}>
            <KeyRound size={11} /> {showCloneAuth ? 'Hide token (optional)' : 'Token for private HTTPS repos (optional)…'}
          </button>
          {showCloneAuth && (
            <div className="mt-2">
              <Input value={clone.token} onChange={(e) => setClone((c) => ({ ...c, token: e.target.value }))} placeholder="Token (optional, stored, never shown)" spellCheck={false} type="password" className="font-mono !text-[12px]" />
            </div>
          )}
        </div>
        <div className="mt-2">
          <Button variant="emerald" className="!py-1.5 text-[11px]" onClick={doClone} disabled={cloneBusy || !clone.name.trim() || !clone.remote.trim()}>
            {cloneBusy ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />} Clone & import
          </Button>
        </div>
      </Card>
    </div>
  );
}
