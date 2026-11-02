//go:build windows

package admin

import "net"

func protectListener(listener net.Listener) net.Listener { return listener }
