//go:build windows

package config

import "github.com/takezoh/credproxy/internal/nativeipc"

func defaultListenPipe() (string, error) { return nativeipc.DefaultPipePath("broker") }
