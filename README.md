# SkillsMCP

[![CI](https://github.com/iMohamedSheta/skillsmcp/actions/workflows/ci.yml/badge.svg)](https://github.com/iMohamedSheta/skillsmcp/actions/workflows/ci.yml)
[![Release](https://github.com/iMohamedSheta/skillsmcp/actions/workflows/release.yml/badge.svg)](https://github.com/iMohamedSheta/skillsmcp/releases/latest)

One binary, no installer. Your prompt skills, callable as MCP tools —
global skills on the main MCP, project skills on their own MCP.

```
You ──desktop app──▶ skills.db ──MCP──▶ AI agent
                         │
         main MCP:  SkillsMCP mcp                 → globals
         project:   SkillsMCP mcp --project <slug> → globals + project
```

Skills are Markdown instructions you write once and reuse everywhere.
Toggle a skill on and its MCP tool goes live instantly — no restart,
no redeploy, no config edit.

![SkillsMCP home — all skills, global live on the main MCP](docs/screenshot-home.png)

## Download

Open the [**latest release**](https://github.com/iMohamedSheta/skillsmcp/releases/latest)
and pick your OS. Per-OS screenshots (`screenshot-windows/macos/linux.png`) ship next
to the binaries — all seeded defaults, never your real skills.

| OS | File | Run |
|---|---|---|
| Windows 10/11 (x64) | `SkillsMCP-Windows-amd64.exe` (`SkillsMCP.exe` is the same file) | Double-click. If SmartScreen warns (unsigned): **More info → Run anyway** |
| macOS (Universal: Intel + Apple Silicon) | `SkillsMCP-macOS-universal.zip` | Unzip, drag `SkillsMCP.app` to Applications, right-click → Open on first launch (unsigned) |
| Linux (x64) | `SkillsMCP-Linux-amd64.tar.gz` | `tar xzf … && ./SkillsMCP` (needs WebKitGTK on minimal distros: `libwebkit2gtk-4.1`) |

Data lives in `~/.skillsmcp/skills.db` on every OS (`%USERPROFILE%\.skillsmcp\skills.db`
on Windows). Override the folder with `SKILLSMCP_HOME` (also used to run an isolated copy).

`SkillsMCP --version` prints the embedded release tag (`dev` for local builds;
Settings shows it too). `SkillsMCP mcp [--project <slug>]` runs the MCP server on stdio.

## Updating

The app checks [GitHub Releases](https://github.com/iMohamedSheta/skillsmcp/releases/latest)
for a newer build: once a day on startup (Settings → General toggles
it off), plus a manual **Check now** button. When an update is found a banner
appears under the menu with the release notes.

- **Windows / Linux:** one click — **Download & install** fetches the asset to
  `~/Downloads`, swaps the binary, and restarts the app on the new version.
- **macOS:** the `.zip` is downloaded to `~/Downloads`; unzip it and drag
  `SkillsMCP.app` to Applications (same unsigned-app flow as a fresh install).

Downloads only ever come from `github.com` / `*.githubusercontent.com` over
HTTPS, and the banner offers **Skip this version** per release. Forks can point
the checker at their own repo with `SKILLSMCP_UPDATE_REPO=owner/repo`.

## Contents

- [How it works](#how-it-works)
- [Screenshots](#screenshots)
- [MCP install](#mcp-install)
- [Tools](#tools)
- [Importing skills](#importing-skills)
- [Data, troubleshooting](#data-troubleshooting)
- [Build from source](#build-from-source)
- [Releasing](#releasing)
- [Project layout](#project-layout)

## How it works

- **Desktop app edits skills** — name, description, Markdown content, category,
  tags, scope (global vs project). Everything is stored in `~/.skillsmcp/skills.db`
  (SQLite WAL, one file, no server).
- **MCP serves over stdio** (`SkillsMCP.exe mcp` — same binary, no window)
  and HTTP (`127.0.0.1:9423/mcp` while the app is open).
- **Tools are dynamic**: `tools/list` is rebuilt from SQLite on every call,
  so a skill you create, edit, or toggle in the UI goes live instantly —
  no restart, no rebuild.
- **Global vs project**: global skills ride the main MCP (`SkillsMCP mcp`);
  each project gets its own MCP (`SkillsMCP mcp --project <slug>`) serving
  globals + that project's skills. Install it alongside the main block under
  a different key (`skillsmcp-<slug>`).
- **Enable toggle = publish switch**: disabling a skill hides its MCP tool
  on the next `tools/list`. Deleting a project deletes its skills too —
  move anything worth keeping to Global first.
- **Default seed**: `git-commit` (Conventional Commits). Add more from the UI
  with **New Skill** or bulk-import `.md` files.

## Screenshots

### Home — all skills, global live on the main MCP

Filter `All / Global / <project>`, open any card to preview and edit it live.

![SkillsMCP home](docs/screenshot-home.png)

### Projects — each gets its own MCP

One card per project with its skills, `Skill here` / `Import` / `MCP config` actions,
and the exact stdio command (`SkillsMCP mcp --project xqb`).

![SkillsMCP projects](docs/screenshot-projects.png)

### MCP — copy-paste client configs

Main MCP setup (opencode local stdio, Claude Desktop / Cursor / Windsurf) plus
per-project blocks and a self-test button.

![SkillsMCP MCP setup](docs/screenshot-mcp.png)

![SkillsMCP project MCP config](docs/screenshot-project-mcp.png)

### New skill → new MCP tool

Pick Global (main MCP) or a Project (that project's own MCP), set slug, category,
description, tags, and the markdown the AI receives — **Create skill** puts it
live immediately.

![SkillsMCP new skill sheet](docs/screenshot-new-skill.png)

## MCP install

`SkillsMCP → MCP tab → Copy`, or paste manually.

opencode — global (`opencode.json` → `mcp.skillsmcp`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "skillsmcp": {
      "type": "local",
      "command": ["C:\\path\\to\\SkillsMCP.exe", "mcp"],
      "enabled": true
    }
  }
}
```

opencode — project (`opencode.json` → `mcp.skillsmcp-<slug>`, alongside the main block):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "skillsmcp-xqb": {
      "type": "local",
      "command": ["C:\\path\\to\\SkillsMCP.exe", "mcp", "--project", "xqb"],
      "enabled": true
    }
  }
}
```

Claude Desktop / Cursor / Windsurf (`mcpServers` shape):

```json
{
  "mcpServers": {
    "skillsmcp": {
      "command": "C:\\path\\to\\SkillsMCP.exe",
      "args": ["mcp"]
    }
  }
}
```

Verify: `opencode run "using skillsmcp list_skills, what skills are available?"`

## Tools

| Tool | What it does |
|---|---|
| `list_skills` | Index (name + description) — the AI starts here |
| `get_skill {name}` | Full markdown for one skill |
| `list_projects` | Project list (each has its own MCP) |
| `list_project_skills {project}` | Skills in one project |
| `<skill-name>` | Shortcut tool per enabled skill (e.g. `git-commit`), description = skill description |

HTTP fallback while the app is open: `POST http://127.0.0.1:9423/mcp`.

Recommended agent workflow: `list_skills` → pick the match → `get_skill(name)`
(or call `<skill-name>` directly) → follow the returned Markdown.

## Importing skills

**Import** (Home header, or per-project) bulk-loads `.md` files as skills:

- Filename becomes the skill slug (`my-deploy.md` → `my-deploy`).
- First `# heading` becomes the description fallback; `# Skill: name — description`
  headers and `> quote` / frontmatter descriptions are parsed when present.
- Choose **Global** (main MCP) or a **Project** (that project's MCP) as the target.
- Duplicates are skipped with a count, not overwritten.

## Data, troubleshooting

- Store: `~/.skillsmcp/skills.db` (SQLite WAL). Delete it to reset to the `git-commit` seed.
  Override the folder with `SKILLSMCP_HOME` (also used for isolated screenshot profiles).
- Logs: in-app **Logs** tab (Settings → Logs shows the path). Backend errors land there —
  reproduce, hit reload, paste the red lines.
- If the app ever shows a blank page: quit **every** SkillsMCP process, then launch fresh
  (Windows: end them in Task Manager. macOS: Cmd+Q. Linux: `pkill SkillsMCP`).
- MCP client can't connect: make sure the command path points at the built
  `build/bin/SkillsMCP.exe`, restart the client session after editing its config,
  and use the in-app **Test MCP** button to verify (it runs the exact stdio handshake
  opencode does).

## Build from source

```bash
go mod tidy
# Windows:
wails build -platform windows/amd64 -o SkillsMCP.exe   # build/bin/SkillsMCP.exe
# macOS (Universal: Intel + Apple Silicon):
wails build -platform darwin/universal -o SkillsMCP    # build/bin/SkillsMCP.app
# Linux:
wails build -platform linux/amd64 -o SkillsMCP         # build/bin/SkillsMCP
wails dev            # desktop + hot reload
cd frontend && npm install && npm run dev   # frontend only
```

Linux needs WebKitGTK dev packages once:
`sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`.

`SkillsMCP --version` prints the baked-in release tag (`dev` for local builds).
`SkillsMCP mcp [--project <slug>]` runs the MCP server on stdio.

## Releasing

Every push to `main` publishes a new GitHub Release automatically
(`.github/workflows/release.yml`): the same version is built on Windows, macOS,
and Linux with the version baked in
(`-ldflags "-X skillsmcp/internal/version.Version=v…"`), each launched against
a fresh isolated profile (`SKILLSMCP_HOME`) and screenshotted while running,
and uploaded as
`SkillsMCP-Windows-amd64.exe` (+ `SkillsMCP.exe` alias),
`SkillsMCP-macOS-universal.zip`, `SkillsMCP-Linux-amd64.tar.gz`,
plus `screenshot.png` (Windows hero) and per-OS shots.

The version bump follows [Conventional Commits](https://www.conventionalcommits.org/):

| Commit message | Bump |
|---|---|
| `feat: ...` / `feat(scope): ...` | minor (`x.Y.0`) |
| `fix: ...` / `fix(scope): ...` | patch (`x.y.Z`) |
| `...!:` / `BREAKING CHANGE` in body | major (`X.0.0`) |
| anything else (`docs:`, `chore:`, …) | patch |

Add `[skip release]` to the HEAD commit message to skip publishing.
Pushes that don't touch the shipped app (docs, `docs/`, `.github/`, `scripts/`)
are skipped automatically — they batch up into the next app release instead.
Pull requests and pushes run `CI` instead: `go build`, `go test`, `go vet`,
`staticcheck` (bug detection) plus `staticcheck -checks "all"` (style lint),
and the frontend `vite build`.

Refresh the docs screenshots any time (fresh isolated profile, never your live skills):

```powershell
# Windows:
./scripts/screenshot.ps1 -OutFile "docs/screenshot-home.png"
```

```bash
# macOS / Linux (Linux CI runs under xvfb-run):
./scripts/screenshot.sh --out "docs/screenshot-home.png"
# Linux headless:
xvfb-run -a ./scripts/screenshot.sh --exe build/bin/SkillsMCP --out docs/screenshot-home.png
```

## Project layout

```
skillsMCP/
  main.go / app.go            # Wails entry, backend bindings (skills, MCP, self-test)
  update_bindings.go          # Wails bindings: CheckForUpdates, DownloadAndInstallUpdate…
  internal/
    model/                    # skill + project types
    store/                    # SQLite (skills.db)
    mcpserver/                # MCP over stdio + HTTP (dynamic tools)
    applog/                   # app logs
    version/                  # release tag baked via ldflags
    update/                   # GitHub Releases check + download + self-install
  frontend/src/               # React UI (home, skill sheet, projects, MCP, logs)
    components/UpdateBanner.tsx  # in-app update banner (release notes, progress)
  scripts/                    # next-version.ps1, screenshot.ps1 (Windows), screenshot.sh (mac/Linux)
  docs/screenshot-*.png       # app screenshots (README + releases)
```
