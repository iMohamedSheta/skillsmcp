//go:build !windows

package gitsync

import "os/exec"

// hideWindow is a no-op outside Windows (no console popups there).
func hideWindow(c *exec.Cmd) {}
