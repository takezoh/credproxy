//go:build !windows

package credproxy

import (
	"fmt"
	"net"
)

func openPipeListener(path, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("credproxy: named pipes are unsupported on this platform: %s", path)
}
