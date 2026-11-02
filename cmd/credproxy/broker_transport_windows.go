//go:build windows

package main

import (
	"os"
	"path/filepath"

	"github.com/takezoh/credproxy/internal/nativeipc"
)

var brokerDialer = nativeipc.DialPipe

func defaultBrokerEndpoint() (string, error) { return nativeipc.DefaultPipePath("broker") }

func brokerEndpointReady(string) error { return nil }

func defaultExecEndpoint() string {
	endpoint, _ := defaultBrokerEndpoint()
	return endpoint
}

func expectedServerExecutable(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), "credproxyd.exe"), nil
}
