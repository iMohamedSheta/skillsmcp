package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"skillsmcp/internal/mcpserver"
	"skillsmcp/internal/store"
	"skillsmcp/internal/version"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v" || os.Args[1] == "version") {
		fmt.Println(version.Version)
		return
	}
	// MCP stdio mode: `SkillsMCP.exe mcp [--workspace slug] [--project slug] [--control]`.
	// No workspace = main (personal) workspace. No project = workspace-main MCP.
	// With --project = that project's MCP (workspace globals + project skills).
	// With --control = the management MCP (skillsmcp-control: all workspaces +
	// write tools). Handled before wails.Run so MCP never touches the GUI lock.
	if len(os.Args) > 1 && (os.Args[1] == "mcp" || os.Args[1] == "--mcp") {
		fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
		dbPath := fs.String("db", "", "path to skills.db (or set SKILLSMCP_DB_PATH)")
		workspace := fs.String("workspace", "", "workspace slug, default main (or set SKILLSMCP_WORKSPACE)")
		project := fs.String("project", "", "project slug for the project MCP (or set SKILLSMCP_PROJECT)")
		control := fs.Bool("control", false, "run the management MCP (or set SKILLSMCP_CONTROL=1)")
		_ = fs.Parse(os.Args[2:])
		sl := *project
		if sl == "" {
			sl = os.Getenv("SKILLSMCP_PROJECT")
		}
		ws := *workspace
		if ws == "" {
			ws = os.Getenv("SKILLSMCP_WORKSPACE")
		}
		ctl := *control || os.Getenv("SKILLSMCP_CONTROL") == "1"
		os.Exit(mcpserver.Run(*dbPath, ws, sl, ctl))
	}
	if v := os.Getenv("SKILLSMCP_MCP"); v == "1" {
		os.Exit(mcpserver.Run("", os.Getenv("SKILLSMCP_WORKSPACE"), os.Getenv("SKILLSMCP_PROJECT"), os.Getenv("SKILLSMCP_CONTROL") == "1"))
	}

	dbPath := os.Getenv("SKILLSMCP_DB_PATH")
	app := NewApp(dbPath)

	err := wails.Run(&options.App{
		Title:     "SkillsMCP",
		Width:     1320,
		Height:    880,
		MinWidth:  960,
		MinHeight: 620,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 9, B: 11, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}

	_ = fmt.Sprintf
	_ = store.ResolveDBPath
}
