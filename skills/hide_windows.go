package skills

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console program from flashing a window up when the
// editor (a GUI program) runs it.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
