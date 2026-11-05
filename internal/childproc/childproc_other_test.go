//go:build !windows

package childproc

import (
	"os/exec"
	"testing"
)

func TestNoWindowLeavesOtherPlatformsUnchanged(t *testing.T) {
	c := exec.Command("true")
	NoWindow(c)
	if c.SysProcAttr != nil {
		t.Fatal("process attributes changed outside Windows")
	}
}
