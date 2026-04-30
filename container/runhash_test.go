package container

import (
	"testing"
)

func TestProjectRunHash_length(t *testing.T) {
	got := ProjectRunHash("/some/project/path")
	if len(got) != 12 {
		t.Errorf("len = %d, want 12: %q", len(got), got)
	}
}

func TestProjectRunHash_deterministic(t *testing.T) {
	if ProjectRunHash("/a") != ProjectRunHash("/a") {
		t.Error("ProjectRunHash is not deterministic")
	}
}

func TestProjectRunHash_distinct(t *testing.T) {
	if ProjectRunHash("/a") == ProjectRunHash("/b") {
		t.Error("distinct paths produced identical hashes")
	}
}
