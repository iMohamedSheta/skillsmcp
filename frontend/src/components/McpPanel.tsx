import { useEffect, useState } from 'react';
import { Copy, Check, Plug2, FlaskConical, Loader2, SquareTerminal, Globe, FolderKanban, Wrench } from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill, Workspace } from '../lib/types';
import { Button, Card, Badge } from './ui';

export default function McpPanel({ skills, projects, mcpUrl, workspace, workspaceId }: { skills: Skill[]; projects: Project[]; mcpUrl: string; workspace: Workspace | null; workspaceId: string }) {
  const [opencode, setOpencode] = useState('loading…');
  const [claude, setClaude] = useState('loading…');
  const [controlOpencode, setControlOpencode] = useState('loading…');
  const [controlClaude, setControlClaude] = useState('loading…');
  const [controlPreview, setControlPreview] = useState('');
  const [copied, setCopied] = useState<string | null>(null);
  const [selftest, setSelftest] = useState('');
  const [testing, setTesting] = useState(false);
  const [controlTest, setControlTest] = useState('');
  const [controlTesting, setControlTesting] = useState(false);
  const [preview, setPreview] = useState('');
  const [projCfgs, setProjCfgs] = useState<Record<string, string>>({});
  const [projTest, setProjTest] = useState<Record<string, string>>({});
  const [openProj, setOpenProj] = useState<string | null>(null);

  const globals = skills.filter((s) => s.enabled && s.scope !== 'project');
  const isMain = !workspace || workspace.isMain;
  const wsSlug = workspace?.slug || 'main';

  function projKey(p: Project): string {
    return isMain ? `skillsmcp-${p.slug}` : `skillsmcp-${wsSlug}-${p.slug}`;
  }

  function projCommand(p: Project): string {
    return isMain ? `SkillsMCP mcp --project ${p.slug}` : `SkillsMCP mcp --workspace ${wsSlug} --project ${p.slug}`;
  }

  useEffect(() => {
    const oc = isMain ? api.OpencodeConfig() : (api as any).OpencodeConfigForWorkspace(workspaceId);
    const cc = isMain ? api.ClaudeConfig() : (api as any).ClaudeConfigForWorkspace(workspaceId);
    const pv = isMain ? api.MCPToolsPreview() : (api as any).WorkspaceMCPToolsPreview(workspaceId);
    Promise.resolve(oc).then((v) => setOpencode(String(v))).catch(() => {});
    Promise.resolve(cc).then((v) => setClaude(String(v))).catch(() => {});
    Promise.resolve(pv).then((v) => setPreview(String(v))).catch(() => {});
    api.OpencodeConfigControl().then((v) => setControlOpencode(String(v))).catch(() => {});
    api.ClaudeConfigControl().then((v) => setControlClaude(String(v))).catch(() => {});
    api.ControlMCPToolsPreview().then((v) => setControlPreview(String(v))).catch(() => {});
    setProjCfgs({});
    setOpenProj(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [skills.length, projects.length, workspaceId]);

  useEffect(() => {
    if (!openProj) return;
    if (!projCfgs[openProj]) {
      const p = projects.find((x) => x.id === openProj);
      if (!p) return;
      ((api as any).OpencodeConfigForProjectIn(workspaceId, p.slug) as Promise<string>)
        .then((v) => setProjCfgs((c) => ({ ...c, [openProj]: String(v) }))).catch(() => {});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openProj ]);

  function copy(id: string, text: string) {
    navigator.clipboard.writeText(text);
    setCopied(id);
    setTimeout(() => setCopied((c) => (c === id ? null : c)), 1200);
  }

  async function runSelftest() {
    setTesting(true);
    setSelftest('');
    try {
      setSelftest(String(await api.TestMCP()));
    } catch (e: any) {
      setSelftest('self-test failed: ' + (e?.message || String(e)));
    } finally {
      setTesting(false);
    }
  }

  async function runControlTest() {
    setControlTesting(true);
    setControlTest('');
    try {
      setControlTest(String(await api.TestControlMCP()));
    } catch (e: any) {
      setControlTest('self-test failed: ' + (e?.message || String(e)));
    } finally {
      setControlTesting(false);
    }
  }

  async function runProjTest(p: Project) {
    setProjTest((t) => ({ ...t, [p.id]: 'testing…' }));
    try {
      const out = String(await (api as any).TestProjectMCPIn(workspaceId, p.slug));
      setProjTest((t) => ({ ...t, [p.id]: out }));
    } catch (e: any) {
      const msg = 'failed: ' + (e?.message || String(e));
      setProjTest((t) => ({ ...t, [p.id]: msg }));
    }
  }

  const base = (mcpUrl || 'http://127.0.0.1:9423').replace(/\/$/, '');

  function Block({ id, title, file, json }: { id: string; title: string; file: string; json: string }) {
    return (
      <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-2.5">
        <div className="mb-1 flex items-center gap-2">
          <span className="text-[12px] font-semibold text-zinc-100">{title}</span>
          <span className="truncate font-mono text-[10px] text-zinc-500">{file}</span>
          <button onClick={() => copy(id, json)}
            className="ml-auto flex shrink-0 items-center gap-1 rounded-md border border-zinc-700 px-1.5 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            {copied === id ? <Check size={11} /> : <Copy size={11} />} {copied === id ? 'Copied' : 'Copy'}
          </button>
        </div>
        <pre className="max-h-56 overflow-auto rounded-lg codeblock bg-black/60 p-2.5 font-mono text-[11px] leading-relaxed text-emerald-200/90">{json}</pre>
      </div>
    );
  }

  return (
    <div className="mx-auto grid max-w-5xl gap-3">
      <Card className="flex flex-wrap items-center gap-3 p-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-500/15 text-emerald-300"><Plug2 size={18} /></div>
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-semibold text-white">
            {isMain ? 'Main MCP — global skills for every agent' : `Workspace MCP — ${workspace?.name} globals`}
          </div>
          <div className="mt-0.5 text-xs text-zinc-500">
            <code className="font-mono text-zinc-300">{isMain ? 'SkillsMCP mcp' : `SkillsMCP mcp --workspace ${wsSlug}`}</code> over stdio · <code className="font-mono text-zinc-300">{base}/mcp</code> over HTTP while open
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <Badge tone="green"><span className="h-1.5 w-1.5 rounded-full bg-emerald-400" /> stdio · local</Badge>
          <Badge tone="indigo">{4 + globals.length} tools</Badge>
          <Button variant="outline" className="!py-1.5 text-[11px]" onClick={runSelftest} disabled={testing}>
            {testing ? <Loader2 size={12} className="animate-spin" /> : <FlaskConical size={12} />} {testing ? 'Testing…' : 'Test MCP'}
          </Button>
        </div>
        {selftest && (
          <pre className="max-h-56 w-full overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2.5 font-mono text-[10px] leading-relaxed text-zinc-300">{selftest}</pre>
        )}
      </Card>

      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100"><SquareTerminal size={14} className="acc-text" /> {isMain ? 'Main MCP setup (global)' : `Workspace MCP setup (${wsSlug})`}</div>
        <div className="mb-2.5 text-[11px] text-zinc-500">Paste into your client, restart its session, then ask it to call <code className="font-mono text-emerald-300">list_skills</code>.</div>
        <div className="grid gap-2">
          <Block id="opencode" title="opencode · local stdio (recommended)" file={isMain ? 'opencode.json → mcp.skillsmcp' : `opencode.json → mcp.${isMain ? 'skillsmcp' : `skillsmcp-${wsSlug}`}`} json={opencode} />
          <Block id="claude" title="Claude Desktop · Cursor · Windsurf" file={isMain ? 'claude_desktop_config.json → mcpServers.skillsmcp' : `claude_desktop_config.json → mcpServers.skillsmcp-${wsSlug}`} json={claude} />
          <div className="flex items-start gap-2 rounded-lg border border-zinc-800 bg-zinc-950 p-2.5 text-[11px] text-zinc-400">
            <Globe size={12} className="mt-0.5 shrink-0 text-zinc-500" />
            <span>HTTP fallback: <code className="font-mono text-zinc-200">POST {base}/mcp</code> — app must stay open.</span>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100"><Wrench size={14} className="text-amber-300" /> Control MCP — let an agent manage the app</div>
        <div className="mb-2.5 text-[11px] text-zinc-500">
          <code className="font-mono text-emerald-300">SkillsMCP mcp --control</code> serves every skill in every workspace plus write tools
          (<code className="font-mono text-zinc-300">app_help · create/update/delete/enable skills & projects · export/import · workspaces · git push/pull · clone</code>).
          Install it <span className="text-zinc-300">alongside</span> the main block under <code className="font-mono text-zinc-300">skillsmcp-control</code> —
          everyday sessions stay read-only, management sessions opt in.
        </div>
        <div className="grid gap-2">
          <Block id="control-opencode" title="opencode · control MCP" file="opencode.json → mcp.skillsmcp-control" json={controlOpencode} />
          <Block id="control-claude" title="Claude Desktop · Cursor · Windsurf" file="claude_desktop_config.json → mcpServers.skillsmcp-control" json={controlClaude} />
          <div className="font-mono text-[10px] text-zinc-600">command: SkillsMCP mcp --control</div>
          <div className="flex items-center gap-1.5">
            <Button variant="outline" className="!py-1 text-[11px]" onClick={runControlTest} disabled={controlTesting}>
              {controlTesting ? <Loader2 size={11} className="animate-spin" /> : <FlaskConical size={11} />} {controlTesting ? 'Testing…' : 'Test control MCP'}
            </Button>
            <Badge tone="indigo">{controlPreview ? `${controlPreview.split('·').length} tools` : '…'}</Badge>
          </div>
          {controlTest && (
            <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2 font-mono text-[10px] text-zinc-300">{controlTest}</pre>
          )}
          <div className="font-mono text-[11px] text-zinc-500">{controlPreview || 'loading…'}</div>
        </div>
      </Card>

      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100"><FolderKanban size={14} className="text-indigo-300" /> Project MCPs — one per project</div>
        <div className="mb-2.5 text-[11px] text-zinc-500">
          Each project gets its <span className="text-zinc-300">own MCP</span> serving <span className="text-zinc-300">workspace globals + that project's skills</span>
          {isMain
            ? (<> (<code className="font-mono text-emerald-300">mcp --project &lt;slug&gt;</code>)</>)
            : (<> (<code className="font-mono text-emerald-300">mcp --workspace {wsSlug} --project &lt;slug&gt;</code>)</>)}.
          Install it <span className="text-zinc-300">alongside</span> the main block under a different key.
        </div>
        {projects.length === 0 && (
          <div className="rounded-lg border border-dashed border-zinc-800 p-3 text-center text-[11px] text-zinc-600">
            No projects yet — create one in the Projects tab, then its MCP config appears here.
          </div>
        )}
        <div className="grid gap-2">
          {projects.map((p) => {
            const count = skills.filter((s) => s.enabled && s.projectId === p.id).length;
            const gcount = globals.length;
            const open = openProj === p.id;
            return (
              <div key={p.id} className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950">
                <button onClick={() => setOpenProj(open ? null : p.id)}
                  className="flex w-full items-center gap-2 px-2.5 py-2 text-left">
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: p.color || '#52525b' }} />
                  <code className="shrink-0 font-mono text-[12px] text-emerald-300">{projKey(p)}</code>
                  <span className="truncate text-[11px] text-zinc-500">{gcount} globals + {count} project skills</span>
                  <span className="ml-auto shrink-0 font-mono text-[10px] text-zinc-600">{open ? '▾' : '▸'}</span>
                </button>
                {open && (
                  <div className="grid gap-2 border-t border-zinc-800 p-2.5">
                    <Block id={`op-${p.id}`} title={`opencode · ${p.slug}`} file={`opencode.json → mcp.${projKey(p)}`}
                      json={projCfgs[p.id] || 'loading…'} />
                    <div className="font-mono text-[10px] text-zinc-600">command: {projCommand(p)}</div>
                    <div className="flex gap-1.5">
                      <Button variant="outline" className="!py-1 text-[11px]" onClick={() => runProjTest(p)}>
                        <FlaskConical size={11} /> Test project MCP
                      </Button>
                    </div>
                    {projTest[p.id] && (
                      <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2 font-mono text-[10px] text-zinc-300">{projTest[p.id]}</pre>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </Card>

      <Card className="p-4">
        <div className="mb-2 font-mono text-[11px] text-zinc-500">{preview || 'loading…'}</div>
        <div className="grid gap-1.5">
          {globals.map((s) => (
            <div key={s.id} className="rounded-lg border border-zinc-800 bg-zinc-950 p-2.5">
              <div className="flex items-center gap-2">
                <code className="font-mono text-[12px] text-emerald-300">{s.name}</code>
                <Badge tone="green">global</Badge>
                {s.category && <Badge tone="zinc">{s.category}</Badge>}
              </div>
              <div className="mt-0.5 text-[11px] text-zinc-400">{s.description}</div>
            </div>
          ))}
          {globals.length === 0 && (
            <div className="rounded-lg border border-dashed border-zinc-800 p-3 text-center text-[11px] text-zinc-600">
              No enabled global skills.
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
