//go:build !windows

package main

import (
	"os"
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

func TestPodcastLockTightensExistingPermissions(t *testing.T) {
	selected := podcast.Podcast{
		ShortTitle: "existing-lock-test",
		Directory:  t.TempDir(),
	}
	path := podcastLockPath(selected)
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
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
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("lock permissions = %#o, want 0600", permissions)
	}
}
