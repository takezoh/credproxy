//go:build !windows

package childproc

import "os/exec"

func noWindow(*exec.Cmd) {}
