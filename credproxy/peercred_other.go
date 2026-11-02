//go:build !linux && !darwin && !windows

package credproxy

import "net"

// peerCredSupported reports whether this platform can read peer credentials.
const peerCredSupported = false
const peerSIDSupported = false

func peerUIDIsCurrent(peerCred) bool { return false }

// peerCredOf is unsupported on this platform; callers that require peer identity
// must fail closed (New refuses RequireSamePeerUID here).
func peerCredOf(_ *net.UnixConn) (peerCred, error) {
	return peerCred{}, errPeerCredUnsupported
}
