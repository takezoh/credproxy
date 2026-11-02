package nativeipc

import (
	"fmt"
	"strings"
)

// PipePath scopes broker and admin endpoints to one explicit Windows identity.
func PipePath(kind, sid string) (string, error) {
	if kind != "broker" && kind != "admin" {
		return "", fmt.Errorf("unsupported pipe kind %q", kind)
	}
	if !validSIDText(sid) {
		return "", fmt.Errorf("invalid SID")
	}
	return `\\.\pipe\credproxyd-` + kind + "-" + sid, nil
}

// SIDMatches compares Windows SID strings without treating an absent identity as one.
func SIDMatches(actual, expected string) bool {
	return actual != "" && expected != "" && strings.EqualFold(actual, expected)
}

// verifyServerSID authenticates the named-pipe server as the current user.
// Executable file ownership is checked separately by the managed-image policy.
func verifyServerSID(serverSID, currentSID string) error {
	if !SIDMatches(serverSID, currentSID) {
		return fmt.Errorf("named pipe server process SID does not match the current user")
	}
	return nil
}

func validSIDText(sid string) bool {
	if len(sid) < 5 || !strings.EqualFold(sid[:2], "S-") {
		return false
	}
	for _, r := range sid[2:] {
		if (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
