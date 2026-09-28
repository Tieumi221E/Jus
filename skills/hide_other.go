//go:build !windows

package skills

import "os/exec"

func hideWindow(*exec.Cmd) {}
