//go:build !linux && !darwin

package credproxy

import "net"

// peerCredSupported reports whether this platform can read peer credentials.
const peerCredSupported = false

// peerCredOf is unsupported on this platform; callers that require peer identity
// must fail closed (New refuses RequireSamePeerUID here).
func peerCredOf(_ *net.UnixConn) (peerCred, error) {
	return peerCred{}, errPeerCredUnsupported
}
