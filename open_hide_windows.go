//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole stops editor launches (code.cmd shims etc.) from flashing a
// console window before the editor opens. GUI apps have no console to
// inherit, so Windows would otherwise pop one per child process.
func hideConsole(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.HideWindow = true
}
