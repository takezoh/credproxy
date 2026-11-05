//go:build windows

package childproc

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNoWindowAddsCreateNoWindowAndKeepsExistingFlags(t *testing.T) {
	c := exec.Command("cmd.exe", "/d", "/c", "echo ok")
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	NoWindow(c)
	if c.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("CREATE_NO_WINDOW was not set")
	}
	if c.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("existing creation flags were dropped")
	}
	if !c.SysProcAttr.HideWindow {
		t.Fatal("HideWindow was not set")
	}
	out, err := c.Output()
	if err != nil {
		t.Fatalf("helper without a window did not run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("helper output was not captured: %q", out)
	}
}

func TestNoWindowCreatesProcessAttributesWhenMissing(t *testing.T) {
	c := exec.Command("cmd.exe")
	NoWindow(c)
	if c.SysProcAttr == nil || c.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("CREATE_NO_WINDOW was not set on a command without attributes")
	}
}
