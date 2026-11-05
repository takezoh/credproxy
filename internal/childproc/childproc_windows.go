//go:build windows

package childproc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func noWindow(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	c.SysProcAttr.HideWindow = true
}
