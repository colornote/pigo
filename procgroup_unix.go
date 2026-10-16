//go:build !windows && !wasip1

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so a timeout can
// kill the entire tree (bash + grandchildren).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup SIGKILLs the whole process group of the child.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
