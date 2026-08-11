//go:build linux

package credproxy

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerCredSupported reports whether this platform can read peer credentials.
const peerCredSupported = true

// peerCredOf reads SO_PEERCRED from a Unix connection (Linux).
func peerCredOf(uc *net.UnixConn) (peerCred, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}, err
	}
	var ucred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return peerCred{}, err
	}
	if sockErr != nil {
		return peerCred{}, sockErr
	}
	return peerCred{UID: ucred.Uid, PID: ucred.Pid, available: true}, nil
}
