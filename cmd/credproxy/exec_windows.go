//go:build windows

package main

import (
	"os"
	"os/exec"
)

func execProcess(bin string, args, environment []string) error {
	cmd := exec.Command(bin, args[1:]...)
	cmd.Env = environment
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func commandSignals() []os.Signal { return []os.Signal{os.Interrupt} }
