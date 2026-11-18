//go:build darwin

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

// peerCredOf reads the peer UID from a Unix connection (macOS via LOCAL_PEERCRED).
// Darwin exposes no peer PID through this path, so PID stays zero.
func peerCredOf(uc *net.UnixConn) (peerCred, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}, err
	}
	var xucred *unix.Xucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		xucred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return peerCred{}, err
	}
	if sockErr != nil {
		return peerCred{}, sockErr
	}
	return peerCred{UID: xucred.Uid, available: true}, nil
}
