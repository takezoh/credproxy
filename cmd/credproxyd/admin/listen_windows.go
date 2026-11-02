//go:build windows

package admin

import (
	"fmt"
	"net"

	"github.com/takezoh/credproxy/internal/nativeipc"
)

// DefaultSocketPath returns the identity-scoped admin named-pipe endpoint.
func DefaultSocketPath() (string, error) { return nativeipc.DefaultPipePath("admin") }

func listen(endpoint string) (net.Listener, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("admin named-pipe endpoint is empty")
	}
	return nativeipc.ListenPipe(endpoint, "")
}

func cleanupEndpoint(string) {}
