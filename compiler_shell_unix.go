// Unix shell for the compiler hook: the command string runs as
// `sh -c "<command>"`, so it is written exactly as at a POSIX prompt. The
// child gets its own process group; on timeout the whole group is killed so a
// wrapper script cannot leave the real compiler running.

//go:build !windows

package main

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}
