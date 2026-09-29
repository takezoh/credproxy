//go:build !linux

package admin

import "net"

type uidListener struct{ net.Listener }
