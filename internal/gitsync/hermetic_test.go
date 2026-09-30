package gitsync

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain runs the whole gitsync suite without any machine git identity:
// global/system configs are pointed at empty temp files so tests exercise
// the same "fresh machine" path CI hits (and would otherwise only fail
// there while passing on developers' configured laptops).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitsync-hermetic")
	if err == nil {
		global := filepath.Join(dir, "gitconfig")
		system := filepath.Join(dir, "gitconfig-system")
		_ = os.WriteFile(global, nil, 0o600)
		_ = os.WriteFile(system, nil, 0o600)
		_ = os.Setenv("GIT_CONFIG_GLOBAL", global)
		_ = os.Setenv("GIT_CONFIG_SYSTEM", system)
		defer os.RemoveAll(dir)
	}
	os.Exit(m.Run())
}
