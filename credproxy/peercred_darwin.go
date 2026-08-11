//go:build darwin

package credproxy

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerCredSupported reports whether this platform can read peer credentials.
const peerCredSupported = true

// peerCredOf reads the peer UID from a Unix connection (macOS via getpeereid).
// Darwin exposes no peer PID through this path, so PID stays zero.
func peerCredOf(uc *net.UnixConn) (peerCred, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}, err
	}
	var uid, gid uint32
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		uid, gid, sockErr = unix.Getpeereid(int(fd))
	}); err != nil {
		return peerCred{}, err
	}
	if sockErr != nil {
		return peerCred{}, sockErr
	}
	_ = gid
	return peerCred{UID: uid, available: true}, nil
}
