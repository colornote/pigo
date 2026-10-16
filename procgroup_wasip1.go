//go:build wasip1

package main

import "os/exec"

// WASI has no process groups and no signal delivery; spawning processes is
// unsupported anyway, so these are no-ops.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {}
