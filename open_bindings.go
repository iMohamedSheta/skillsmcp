package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"skillsmcp/internal/gitsync"
	"skillsmcp/internal/store"
)

// This file holds the "escape hatch" bindings behind the Sync tab's
// Open folder / Terminal / Editor buttons. Everything is constrained to
// the workspace checkout dir so the UI can never be tricked into opening
// arbitrary paths.

// WorkspaceCheckoutDir returns the local checkout path for a workspace.
// The dir is created when missing so "open folder" always lands somewhere.
func (a *App) WorkspaceCheckoutDir(id string) (string, error) {
	w, ok := a.store.GetWorkspace(id)
	if !ok {
		return "", fmt.Errorf("workspace not found")
	}
	dir := gitsync.DirFor(store.AppDir(), w.Slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// OpenCheckoutFolder reveals the workspace checkout in the file manager.
func (a *App) OpenCheckoutFolder(id string) error {
	dir, err := a.WorkspaceCheckoutDir(id)
	if err != nil {
		return err
	}
	return openPath(dir)
}

// OpenCheckoutTerminal launches a terminal rooted at the workspace checkout
// so diverged-history / auth problems can be fixed by hand
// (git status, git pull --rebase, git push --force-with-lease, ...).
func (a *App) OpenCheckoutTerminal(id string) error {
	dir, err := a.WorkspaceCheckoutDir(id)
	if err != nil {
		return err
	}
	return openTerminal(dir)
}

// OpenCheckoutFile opens one checkout-relative file (e.g.
// globals/skills/foo.md) in VS Code when available, else the OS default
// handler. rel is always resolved inside the checkout — ".." escapes are
// rejected.
func (a *App) OpenCheckoutFile(id string, rel string) error {
	dir, err := a.WorkspaceCheckoutDir(id)
	if err != nil {
		return err
	}
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return openEditor(dir)
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to open %q outside the checkout", rel)
	}
	target := filepath.Join(dir, clean)
	return openEditor(target)
}

// OpenCheckoutEditor opens the checkout root in the editor (VS Code when
// available, else the file manager).
func (a *App) OpenCheckoutEditor(id string) error {
	dir, err := a.WorkspaceCheckoutDir(id)
	if err != nil {
		return err
	}
	return openEditor(dir)
}

func openPath(path string) error {
	switch goruntime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func openEditor(path string) error {
	// Prefer an editor the user likely resolves conflicts in; fall back
	// to the OS default handler for the path. The console is hidden so
	// .cmd shims (code, cursor, ...) don't flash a terminal first.
	for _, ed := range []string{"code", "cursor", "windsurf"} {
		if exe, err := exec.LookPath(ed); err == nil {
			cmd := exec.Command(exe, path)
			hideConsole(cmd)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}
	return openPath(path)
}

func openTerminal(dir string) error {
	switch goruntime.GOOS {
	case "windows":
		// Windows Terminal when present keeps cwd via -d; otherwise a
		// classic console rooted at the checkout.
		if wt, err := exec.LookPath("wt"); err == nil {
			return exec.Command(wt, "-d", dir).Start()
		}
		return exec.Command("cmd", "/c", "start", "", "cmd", "/k", "cd", "/d", dir).Start()
	case "darwin":
		return exec.Command("open", "-a", "Terminal", dir).Start()
	default:
		for _, term := range [][]string{
			{"x-terminal-emulator", "--working-directory=" + dir},
			{"gnome-terminal", "--working-directory=" + dir},
			{"konsole", "--workdir", dir},
			{"xfce4-terminal", "--working-directory=" + dir},
		} {
			if _, err := exec.LookPath(term[0]); err == nil {
				return exec.Command(term[0], term[1:]...).Start()
			}
		}
		if _, err := exec.LookPath("xterm"); err == nil {
			return exec.Command("xterm", "-e", "sh", "-c", fmt.Sprintf("cd %q; exec sh", dir)).Start()
		}
		return fmt.Errorf("no terminal emulator found (tried x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal, xterm)")
	}
}
