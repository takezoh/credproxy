//go:build windows

package credproxy

import (
	"net"

	"github.com/takezoh/credproxy/internal/nativeipc"
)

const peerCredSupported = false
const peerSIDSupported = true

func peerUIDIsCurrent(peerCred) bool { return false }

func peerCredOf(*net.UnixConn) (peerCred, error) {
	return peerCred{}, errPeerCredUnsupported
}

func openPipeListener(path, peerSID string) (net.Listener, error) {
	return nativeipc.ListenPipe(path, peerSID)
}
