//go:build unix

package engine

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel runs the engine in its own process group and kills the
// whole group on timeout or cancel: Chrome and uv leave children behind that
// would otherwise hold the output open and outlive imgkit.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// killGroup kills whatever is left of the engine's process group once the
// engine itself has exited. An empty group (ESRCH) is the normal case, and
// no other failure changes the call's outcome, so the error is dropped.
func killGroup(cmd *exec.Cmd) {
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
