//go:build !windows

package main

import (
	"testing"

	"gopod/podcast"
)

func TestPodcastLockFileIsOwnerOnly(t *testing.T) {
	selected := podcast.Podcast{
		ShortTitle: "private-lock-test",
		Directory:  t.TempDir(),
	}
	lock, err := acquirePodcastLock(selected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Errorf("release podcast lock: %v", err)
		}
	})

	info, err := lock.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		t.Fatalf("lock permissions = %#o, want no group or other access", permissions)
	}
}
