package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopod/podcast"
)

func TestPodcastLockRejectsConcurrentProcess(t *testing.T) {
	selected := podcast.Podcast{
		ShortTitle: "lock-test",
		Directory:  t.TempDir(),
	}
	readyPath := filepath.Join(t.TempDir(), "ready")
	command := podcastLockHelperCommand(selected, "hold", readyPath)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	waitForLockReady(t, readyPath, &stderr)

	lock, err := acquirePodcastLock(selected)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("second process unexpectedly acquired the podcast lock")
	}
	if !errors.Is(err, errPodcastLocked) {
		t.Fatalf("got error %v, want errPodcastLocked", err)
	}
	if !strings.Contains(err.Error(), podcastLockPath(selected)) {
		t.Fatalf("lock error %q does not include lock path", err)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock helper failed: %v\n%s", err, stderr.String())
	}

	lock, err = acquirePodcastLock(selected)
	if err != nil {
		t.Fatalf("acquire released podcast lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release podcast lock: %v", err)
	}
}

func TestPodcastLockIsReleasedAfterProcessCrash(t *testing.T) {
	selected := podcast.Podcast{
		ShortTitle: "crash-test",
		Directory:  t.TempDir(),
	}
	command := podcastLockHelperCommand(selected, "crash", "")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("crashing lock helper failed: %v\n%s", err, stderr.String())
	}

	if _, err := os.Stat(podcastLockPath(selected)); err != nil {
		t.Fatalf("stat persistent lock file: %v", err)
	}
	lock, err := acquirePodcastLock(selected)
	if err != nil {
		t.Fatalf("acquire lock after holder crash: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release podcast lock: %v", err)
	}
}

func TestPodcastLockHelper(t *testing.T) {
	if os.Getenv("GOPOD_TEST_LOCK_HELPER") != "1" {
		return
	}

	selected := podcast.Podcast{
		ShortTitle: os.Getenv("GOPOD_TEST_LOCK_SHORT_TITLE"),
		Directory:  os.Getenv("GOPOD_TEST_LOCK_DIRECTORY"),
	}
	lock, err := acquirePodcastLock(selected)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if readyPath := os.Getenv("GOPOD_TEST_LOCK_READY"); readyPath != "" {
		if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
	}

	switch os.Getenv("GOPOD_TEST_LOCK_MODE") {
	case "hold":
		_, _ = io.Copy(io.Discard, os.Stdin)
		if err := lock.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
	case "crash":
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown lock helper mode")
		os.Exit(5)
	}
}

func podcastLockHelperCommand(
	selected podcast.Podcast,
	mode string,
	readyPath string,
) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestPodcastLockHelper$")
	command.Env = append(
		os.Environ(),
		"GOPOD_TEST_LOCK_HELPER=1",
		"GOPOD_TEST_LOCK_SHORT_TITLE="+selected.ShortTitle,
		"GOPOD_TEST_LOCK_DIRECTORY="+selected.Directory,
		"GOPOD_TEST_LOCK_MODE="+mode,
		"GOPOD_TEST_LOCK_READY="+readyPath,
	)
	return command
}

func waitForLockReady(t *testing.T, readyPath string, stderr *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock helper did not become ready\n%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
