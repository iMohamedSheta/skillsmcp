package gitsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGitExeSkipsBrokenShim reproduces the Laragon setup: PATH starts with
// a broken git shim (runs but fails / prints garbage, e.g. "BUG (fork
// bomb)"), while a working Git for Windows sits later in PATH. gitExe must
// skip the shim and return a binary that actually runs.
func TestGitExeSkipsBrokenShim(t *testing.T) {
	if _, err := gitExe(); err != nil {
		t.Skipf("no working git on PATH, cannot test fallback: %v", err)
	}
	shimDir := t.TempDir()
	// A shim that runs yet fails like the broken msys launcher.
	shim := filepath.Join(shimDir, "git.cmd")
	if err := os.WriteFile(shim, []byte("@echo BUG (fork bomb) mock\r\nexit /b 1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Drop the cache so this call re-probes with the poisoned PATH.
	gitCache.Lock()
	gitCache.ok = false
	gitCache.exe = ""
	gitCache.Unlock()

	exe, err := gitExe()
	if err != nil {
		t.Fatalf("gitExe with broken shim first: %v", err)
	}
	if strings.HasPrefix(strings.ToLower(exe), strings.ToLower(shimDir)) {
		t.Fatalf("picked the broken shim: %q", exe)
	}
	if !probeGit(exe) {
		t.Fatalf("picked exe does not run: %q", exe)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := CheckGit(ctx); err != nil {
		t.Fatalf("CheckGit with broken shim first: %v", err)
	}
}
