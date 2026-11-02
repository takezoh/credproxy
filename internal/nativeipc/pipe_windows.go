//go:build windows

package nativeipc

import (
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// CurrentUserSID returns the process token's user SID.
func CurrentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read process user SID: %w", err)
	}
	return user.User.Sid.String(), nil
}

// DefaultPipePath returns the local named-pipe endpoint for a fixed broker role.
func DefaultPipePath(kind string) (string, error) {
	sid, err := CurrentUserSID()
	if err != nil {
		return "", err
	}
	return PipePath(kind, sid)
}

// ListenPipe creates a first-instance, local-only pipe with a protected DACL.
func ListenPipe(path, expectedPeerSID string) (net.Listener, error) {
	if err := validatePipePath(path); err != nil {
		return nil, err
	}
	ownerSID, err := CurrentUserSID()
	if err != nil {
		return nil, err
	}
	peerSID, err := canonicalSID(expectedPeerSID, ownerSID)
	if err != nil {
		return nil, err
	}
	descriptor := pipeDescriptor(ownerSID, peerSID)
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: descriptor})
	if err != nil {
		return nil, fmt.Errorf("listen named pipe: %w", err)
	}
	return &sidListener{Listener: listener, expected: peerSID}, nil
}

func canonicalSID(configured, fallback string) (string, error) {
	if configured == "" {
		configured = fallback
	}
	sid, err := windows.StringToSid(configured)
	if err != nil {
		return "", fmt.Errorf("invalid peer SID: %w", err)
	}
	return sid.String(), nil
}

func pipeDescriptor(owner, peer string) string {
	entries := "(A;;GA;;;" + owner + ")(A;;GA;;;SY)"
	if !SIDMatches(owner, peer) {
		entries = "(A;;GA;;;" + peer + ")" + entries
	}
	return "O:" + owner + "G:SYD:P" + entries
}

func validatePipePath(path string) error {
	if !strings.HasPrefix(strings.ToLower(path), `\\.\pipe\`) || strings.Contains(path, "..") {
		return fmt.Errorf(`named pipe path must be a local \\.\pipe endpoint`)
	}
	return nil
}
