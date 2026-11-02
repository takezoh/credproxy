package nativeipc

import "testing"

func TestSIDMatches(t *testing.T) {
	for _, tc := range []struct {
		actual, expected string
		want             bool
	}{
		{"S-1-5-21-1-2-3-1000", "S-1-5-21-1-2-3-1000", true},
		{"s-1-5-21-1-2-3-1000", "S-1-5-21-1-2-3-1000", true},
		{"S-1-5-21-1-2-3-1001", "S-1-5-21-1-2-3-1000", false},
		{"", "S-1-5-21-1-2-3-1000", false},
		{"S-1-5-21-1-2-3-1000", "", false},
	} {
		if got := SIDMatches(tc.actual, tc.expected); got != tc.want {
			t.Errorf("SIDMatches(%q, %q) = %t, want %t", tc.actual, tc.expected, got, tc.want)
		}
	}
}

func TestVerifyServerSIDUsesCurrentUserIndependentlyOfImageOwner(t *testing.T) {
	current := "S-1-5-21-100-200-300-1001"
	if err := verifyServerSID(current, current); err != nil {
		t.Fatalf("accepted current-user server SID: %v", err)
	}
	if err := verifyServerSID("", current); err == nil {
		t.Fatal("accepted a server with missing identity")
	}
	if err := verifyServerSID("S-1-5-18", current); err == nil {
		t.Fatal("accepted a server running as SYSTEM instead of the current user")
	}
}

func TestPipePathUsesSIDAndFixedRole(t *testing.T) {
	path, err := PipePath("broker", "S-1-5-21-1-2-3-1000")
	if err != nil || path != `\\.\pipe\credproxyd-broker-S-1-5-21-1-2-3-1000` {
		t.Fatalf("PipePath = %q, %v", path, err)
	}
	if _, err := PipePath("other", "S-1-5-21-1-2-3-1000"); err == nil {
		t.Fatal("accepted an arbitrary pipe role")
	}
	if _, err := PipePath("broker", "not-a-sid"); err == nil {
		t.Fatal("accepted a non-SID pipe suffix")
	}
}
