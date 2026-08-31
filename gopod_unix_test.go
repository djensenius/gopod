//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopod/podcast"
)

func TestRecordPodcastCancelsRecorderWhenMetadataFails(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(tempDir, "ffmpeg.pid")
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$$\" > \"$GOPOD_TEST_FFMPEG_PID\"\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPOD_TEST_FFMPEG_PID", pidPath)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(pidPath); err == nil {
				break
			}
			if time.Now().After(deadline) {
				http.Error(w, "ffmpeg did not start", http.StatusInternalServerError)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = w.Write([]byte("stream without icy metadata"))
	}))
	t.Cleanup(server.Close)

	err := recordPodcast(context.Background(), podcast.Podcast{
		Title:      "Cancellation test",
		SourceURL:  server.URL,
		Extension:  "aac",
		Length:     30,
		ShortTitle: "cancel",
		Directory:  outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), "monitor podcast stream") {
		t.Fatalf("got error %v, want metadata monitoring failure", err)
	}

	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read ffmpeg PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("parse ffmpeg PID: %v", err)
	}

	waitForRecordedProcessExit(t, pid)
	assertNoRecordingPartials(t, tempDir)
}

func TestMainSIGTERMCancelsWorkAndCleansRecording(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	pidPath := filepath.Join(tempDir, "ffmpeg.pid")
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$$\" > \"$GOPOD_TEST_FFMPEG_PID\"\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(
		_ http.ResponseWriter,
		request *http.Request,
	) {
		requestStarted <- struct{}{}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	selected := podcast.Podcast{
		Title:      "Signal test",
		ShortTitle: "signal",
		Directory:  outputDir,
		SourceURL:  server.URL,
		Extension:  "aac",
		Length:     30,
	}
	configData, err := json.Marshal(podcast.Podcasts{
		Podcasts: []*podcast.Podcast{&selected},
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestMainSignalHelper$")
	var stderr bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &stderr
	command.Env = append(
		os.Environ(),
		"GOPOD_TEST_MAIN_SIGNAL_HELPER=1",
		"GOPOD_TEST_MAIN_SIGNAL_CONFIG="+configPath,
		"GOPOD_TEST_FFMPEG_PID="+pidPath,
		"PATH="+tempDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+tempDir,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("metadata request did not start\n%s", stderr.String())
	}
	pid := waitForRecordedPID(t, pidPath)

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- command.Wait()
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("signal helper unexpectedly exited successfully")
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-result
		t.Fatal("gopod did not exit after SIGTERM")
	}

	if !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Fatalf("stderr %q does not report cancellation", stderr.String())
	}
	waitForRecordedProcessExit(t, pid)
	assertNoRecordingPartials(t, tempDir)

	lock, err := acquirePodcastLock(selected)
	if err != nil {
		t.Fatalf("acquire podcast lock after signal exit: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release podcast lock: %v", err)
	}
}

func TestMainSignalHelper(t *testing.T) {
	if os.Getenv("GOPOD_TEST_MAIN_SIGNAL_HELPER") != "1" {
		return
	}
	os.Args = []string{
		"gopod",
		"--config",
		os.Getenv("GOPOD_TEST_MAIN_SIGNAL_CONFIG"),
		"signal",
	}
	main()
}

func waitForRecordedPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse ffmpeg PID: %v", err)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("ffmpeg did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForRecordedProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("check ffmpeg process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("ffmpeg process %d remained alive", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertNoRecordingPartials(t *testing.T, tempDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(tempDir, "gopod-recording-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("recording partial files remain: %v", matches)
	}
}
