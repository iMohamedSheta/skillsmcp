//go:build !windows

package main

import "os/exec"

// hideConsole is a no-op outside Windows (no console popups there).
func hideConsole(c *exec.Cmd) {}
