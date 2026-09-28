# SkillsMCP — personal skill library + MCP server

Wails + SQLite + React (shadcn-style, ReadGate theme). Each **enabled skill** becomes an **MCP tool** with the same name.

## How it works

- Desktop app edits skills (name, description, markdown content) stored in `~/.skillsmcp/skills.db`.
- MCP serves over **stdio** (`SkillsMCP.exe mcp` — same binary, no window) and HTTP (`127.0.0.1:9423/mcp` while open).
- Tools are **dynamic**: `tools/list` is rebuilt from SQLite on every call.
  - `list_skills` — index (name + description). AI starts here.
  - `get_skill {name}` — full markdown for one skill.
  - `<skill-name>` — shortcut tool per enabled skill (e.g. `git-commit`), description = skill description.
- Default seed: `git-commit` (Conventional Commits). Add more from the UI; they go live instantly.

## Run

```powershell
cd E:\laragon\www\go\skillsMCP\frontend
npm install
npm run build
cd ..
go run .
# or
wails dev
```

## MCP install (opencode — global)

`SkillsMCP → MCP tab → Copy`, or paste manually into `opencode.json`:

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

## Styles

Copied from `ReadGate`: dark zinc-950, accent engine (`--acc`), light-theme overrides, `grid-paper` background, `surf` cards, `bdg` chips, frameless `Menu`, shadcn-like `ui.tsx` (Button/Card/Input/Badge/Sheet/ConfirmModal).
