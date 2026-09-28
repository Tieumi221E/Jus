package apps

import (
	"os/exec"
	"syscall"
)

// hide keeps a GUI-subsystem app asked for its manifest from flashing a console.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
