//go:build unix

package engine

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroupOnCancel runs the engine in its own process group and kills the
// whole group on timeout or cancel: Chrome and uv leave children behind that
// would otherwise hold the output open and outlive imgkit.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
