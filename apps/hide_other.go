//go:build !windows

package apps

import "os/exec"

func hide(*exec.Cmd) {}
