// Windows shell for the compiler hook: the command string runs as
// `cmd.exe /S /C "<command>"`, passed through SysProcAttr.CmdLine so Go's
// argv escaping (MSVC rules, which cmd.exe does not follow) never touches it.
// With /S, cmd strips exactly the outer quotes and runs the rest as typed, so
// quoting works the same as at a cmd prompt. On timeout the whole process
// tree is killed (taskkill /T): killing cmd.exe alone would orphan the real
// compiler (node, python, ...).

//go:build windows

package compile

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	return cmd
}
