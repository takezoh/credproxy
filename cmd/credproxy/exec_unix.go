//go:build !windows

package main

import (
	"os"
	"syscall"
)

func execProcess(bin string, args, environment []string) error {
	return syscall.Exec(bin, args, environment)
}

func commandSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
