//go:build linux

package credproxy

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// peerCredSupported reports whether this platform can read peer credentials.
const peerCredSupported = true
const peerSIDSupported = false

func peerUIDIsCurrent(pc peerCred) bool {
	return authorizePeer(pc, true, uint32(os.Getuid()))
}

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
