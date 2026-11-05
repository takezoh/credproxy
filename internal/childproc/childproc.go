// Package childproc configures helper processes that credproxy starts.
package childproc

import "os/exec"

// NoWindow keeps a console helper from opening a window of its own.
//
// On Windows credproxyd is built without a console so that the Task Scheduler
// does not show a window that stops the daemon when it is closed. A console
// program started from a process without a console gets a new visible window,
// so every helper process must be started with this option. It does nothing on
// other platforms.
func NoWindow(c *exec.Cmd) {
	noWindow(c)
}
