import { useEffect, useState } from 'react';
import { Copy, Check, Plug2, FlaskConical, Loader2, SquareTerminal, Globe, FolderKanban } from 'lucide-react';
import { api } from '../lib/api';
import type { Project, Skill } from '../lib/types';
import { Button, Card, Badge } from './ui';

export default function McpPanel({ skills, projects, mcpUrl }: { skills: Skill[]; projects: Project[]; mcpUrl: string }) {
  const [opencode, setOpencode] = useState('loading…');
  const [claude, setClaude] = useState('loading…');
  const [copied, setCopied] = useState<string | null>(null);
  const [selftest, setSelftest] = useState('');
  const [testing, setTesting] = useState(false);
  const [preview, setPreview] = useState('');
  const [projCfgs, setProjCfgs] = useState<Record<string, string>>({});
  const [projTest, setProjTest] = useState<Record<string, string>>({});
  const [openProj, setOpenProj] = useState<string | null>(null);

  const globals = skills.filter((s) => s.enabled && s.scope !== 'project');

  useEffect(() => {
    api.OpencodeConfig().then((v) => setOpencode(String(v))).catch(() => {});
    api.ClaudeConfig().then((v) => setClaude(String(v))).catch(() => {});
    api.MCPToolsPreview().then((v) => setPreview(String(v))).catch(() => {});
  }, [skills.length, projects.length]);

  useEffect(() => {
    if (!openProj) return;
    if (!projCfgs[openProj]) {
      api.OpencodeConfigForProject(openProj).then((v) => setProjCfgs((c) => ({ ...c, [openProj]: String(v) }))).catch(() => {});
    }
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

  async function runProjTest(slug: string) {
    setProjTest((t) => ({ ...t, [slug]: 'testing…' }));
    try {
      const out = String(await api.TestProjectMCP(slug));
      setProjTest((t) => ({ ...t, [slug]: out }));
    } catch (e: any) {
      const msg = 'failed: ' + (e?.message || String(e));
      setProjTest((t) => ({ ...t, [slug]: msg }));
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
          <div className="text-[15px] font-semibold text-white">Main MCP — global skills for every agent</div>
          <div className="mt-0.5 text-xs text-zinc-500">
            <code className="font-mono text-zinc-300">SkillsMCP mcp</code> over stdio · <code className="font-mono text-zinc-300">{base}/mcp</code> over HTTP while open
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
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100"><SquareTerminal size={14} className="acc-text" /> Main MCP setup (global)</div>
        <div className="mb-2.5 text-[11px] text-zinc-500">Paste into your client, restart its session, then ask it to call <code className="font-mono text-emerald-300">list_skills</code>.</div>
        <div className="grid gap-2">
          <Block id="opencode" title="opencode · local stdio (recommended)" file="opencode.json → mcp.skillsmcp" json={opencode} />
          <Block id="claude" title="Claude Desktop · Cursor · Windsurf" file="claude_desktop_config.json → mcpServers.skillsmcp" json={claude} />
          <div className="flex items-start gap-2 rounded-lg border border-zinc-800 bg-zinc-950 p-2.5 text-[11px] text-zinc-400">
            <Globe size={12} className="mt-0.5 shrink-0 text-zinc-500" />
            <span>HTTP fallback: <code className="font-mono text-zinc-200">POST {base}/mcp</code> — app must stay open.</span>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <div className="mb-1 flex items-center gap-1.5 text-[13px] font-semibold text-zinc-100"><FolderKanban size={14} className="text-indigo-300" /> Project MCPs — one per project</div>
        <div className="mb-2.5 text-[11px] text-zinc-500">
          Each project gets its <span className="text-zinc-300">own MCP</span> (<code className="font-mono text-emerald-300">mcp --project &lt;slug&gt;</code>) serving <span className="text-zinc-300">globals + that project's skills</span>.
          Install it <span className="text-zinc-300">alongside</span> the main block under a different key (<code className="font-mono text-zinc-300">skillsmcp-&lt;slug&gt;</code>).
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
            const open = openProj === p.slug;
            return (
              <div key={p.id} className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950">
                <button onClick={() => setOpenProj(open ? null : p.slug)}
                  className="flex w-full items-center gap-2 px-2.5 py-2 text-left">
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: p.color || '#52525b' }} />
                  <code className="shrink-0 font-mono text-[12px] text-emerald-300">skillsmcp-{p.slug}</code>
                  <span className="truncate text-[11px] text-zinc-500">{gcount} globals + {count} project skills</span>
                  <span className="ml-auto shrink-0 font-mono text-[10px] text-zinc-600">{open ? '▾' : '▸'}</span>
                </button>
                {open && (
                  <div className="grid gap-2 border-t border-zinc-800 p-2.5">
                    <Block id={`op-${p.slug}`} title={`opencode · ${p.slug}`} file={`opencode.json → mcp.skillsmcp-${p.slug}`}
                      json={projCfgs[p.slug] || 'loading…'} />
                    <div className="font-mono text-[10px] text-zinc-600">command: SkillsMCP mcp --project {p.slug}</div>
                    <div className="flex gap-1.5">
                      <Button variant="outline" className="!py-1 text-[11px]" onClick={() => runProjTest(p.slug)}>
                        <FlaskConical size={11} /> Test project MCP
                      </Button>
                    </div>
                    {projTest[p.slug] && (
                      <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-lg border border-zinc-800 bg-black/60 p-2 font-mono text-[10px] text-zinc-300">{projTest[p.slug]}</pre>
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
