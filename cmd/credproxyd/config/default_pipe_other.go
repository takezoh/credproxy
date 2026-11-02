//go:build !windows

package config

func defaultListenPipe() (string, error) { return "", nil }
