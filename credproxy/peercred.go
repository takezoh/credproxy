package credproxy

import (
	"context"
	"net"
	"strings"
)

// peerCred is the identity of the process on the far end of a Unix socket.
type peerCred struct {
	UID       uint32
	PID       int32
	available bool // false for non-Unix connections or unsupported platforms
}

// peerCredKey is the context key for the connection's peer credentials.
type peerCredKey struct{}

type peerSID struct {
	SID       string
	available bool
}

type peerSIDKey struct{}

type peerSIDConnection interface {
	PeerSID() string
}

// connContext captures peer credentials at connection accept time and stashes
// them on the request context. Wired via http.Server.ConnContext.
func connContext(ctx context.Context, c net.Conn) context.Context {
	if sc, ok := c.(peerSIDConnection); ok {
		if sid := sc.PeerSID(); sid != "" {
			ctx = context.WithValue(ctx, peerSIDKey{}, peerSID{SID: sid, available: true})
		}
	}
	if uc, ok := c.(*net.UnixConn); ok {
		if pc, err := peerCredOf(uc); err == nil {
			return context.WithValue(ctx, peerCredKey{}, pc)
		}
	}
	return ctx
}

func authorizeSID(peer peerSID, require bool, expected string) bool {
	if !require {
		return true
	}
	return peer.available && expected != "" && strings.EqualFold(peer.SID, expected)
}

// authorizePeer decides whether a connection may proceed under the same-UID
// policy (pure). When require is false every connection passes. When true the
// connection must carry peer credentials whose UID equals selfUID — a missing
// credential (non-Unix transport, unsupported platform) denies rather than
// silently passing.
func authorizePeer(pc peerCred, require bool, selfUID uint32) bool {
	if !require {
		return true
	}
	if !pc.available {
		return false
	}
	return pc.UID == selfUID
}
