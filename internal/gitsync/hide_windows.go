//go:build windows

package gitsync

import (
	"os/exec"
	"syscall"
)

// hideWindow stops git.exe from flashing a console window on every
// push/pull/status call. GUI apps (like this one) have no console to
// inherit, so Windows would otherwise pop one per git child process.
func hideWindow(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.HideWindow = true
}
