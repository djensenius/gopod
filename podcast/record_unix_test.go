//go:build linux || darwin

package podcast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCombineCancellationCleansPartialAndPreservesCompletedFiles(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	pidPath := filepath.Join(tempDir, "ffmpeg.pid")
	argsPath := filepath.Join(tempDir, "ffmpeg.args")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$GOPOD_TEST_FFMPEG_ARGS\"\n" +
		"output=''\n" +
		"for argument in \"$@\"; do output=\"$argument\"; done\n" +
		"printf 'partial audio' > \"$output\"\n" +
		"printf '%s\\n' \"$$\" > \"$GOPOD_TEST_FFMPEG_PID\"\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPOD_TEST_FFMPEG_PID", pidPath)
	t.Setenv("GOPOD_TEST_FFMPEG_ARGS", argsPath)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	inputAudio := filepath.Join(tempDir, "input.aac")
	inputMetadata := filepath.Join(tempDir, "metadata.txt")
	inputDescription := filepath.Join(tempDir, "description.txt")
	for path, content := range map[string]string{
		inputAudio:       "input audio",
		inputMetadata:    ";FFMETADATA1\n",
		inputDescription: "description",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	preserved := make(map[string]string)
	now := time.Now().Unix()
	for second := now - 1; second <= now+2; second++ {
		for _, extension := range []string{".m4a", ".txt"} {
			path := filepath.Join(tempDir, "cancel-"+strconv.FormatInt(second, 10)+extension)
			content := "preserved " + filepath.Base(path)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			preserved[path] = content
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Combine(
			ctx,
			Podcast{ShortTitle: "cancel", Directory: tempDir},
			inputAudio,
			inputMetadata,
			inputDescription,
		)
	}()

	pid := waitForFFmpegPID(t, pidPath)
	assertCombineFFmpegArguments(t, argsPath)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got error %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Combine did not return after cancellation")
	}
	waitForFFmpegExit(t, pid)

	for path, content := range preserved {
		assertFileContent(t, path, content)
	}
	for _, pattern := range []string{
		filepath.Join(tempDir, ".gopod-audio-*.part"),
		filepath.Join(tempDir, ".gopod-description-*.part"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("owned partial files remain after cancellation: %v", matches)
		}
	}
}

func TestCombineDescriptionReadErrorIncludesPath(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"output=''\n" +
		"for argument in \"$@\"; do output=\"$argument\"; done\n" +
		"printf 'combined audio' > \"$output\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	descriptionPath := filepath.Join(tempDir, "missing-description.txt")
	err := Combine(
		context.Background(),
		Podcast{ShortTitle: "description-error", Directory: tempDir},
		filepath.Join(tempDir, "input.aac"),
		filepath.Join(tempDir, "metadata.txt"),
		descriptionPath,
	)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got error %v, want missing description error", err)
	}
	if !strings.Contains(err.Error(), descriptionPath) {
		t.Fatalf("error %q does not include description path %q", err, descriptionPath)
	}
}

func TestCombineCleanupPreservesReplacementAudioStage(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	outputPathFile := filepath.Join(tempDir, "ffmpeg.output")
	script := "#!/bin/sh\n" +
		"output=''\n" +
		"for argument in \"$@\"; do output=\"$argument\"; done\n" +
		"printf '%s\\n' \"$output\" > \"$GOPOD_TEST_OUTPUT_PATH\"\n" +
		"mv \"$output\" \"$output.owned\"\n" +
		"printf 'replacement audio stage' > \"$output\"\n" +
		"exit 1\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPOD_TEST_OUTPUT_PATH", outputPathFile)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	inputAudio := filepath.Join(tempDir, "input.aac")
	inputMetadata := filepath.Join(tempDir, "metadata.txt")
	for path, content := range map[string]string{
		inputAudio:    "input audio",
		inputMetadata: ";FFMETADATA1\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := Combine(
		context.Background(),
		Podcast{ShortTitle: "cleanup-owner", Directory: tempDir},
		inputAudio,
		inputMetadata,
		"",
	)
	if err == nil {
		t.Fatal("expected ffmpeg and staging ownership errors")
	}
	if !strings.Contains(err.Error(), "run ffmpeg combiner") ||
		!strings.Contains(err.Error(), "refuse to remove staging file") ||
		!strings.Contains(err.Error(), "restored the claimed file") {
		t.Fatalf("error %q does not report atomic-claim audio cleanup", err)
	}

	data, err := os.ReadFile(outputPathFile)
	if err != nil {
		t.Fatal(err)
	}
	audioStage := strings.TrimSpace(string(data))
	assertFileContent(t, audioStage, "replacement audio stage")
	assertFileContent(t, audioStage+".owned", "")
	assertNoOwnedFileCleanupDirectories(t, tempDir)
}

func assertCombineFFmpegArguments(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	arguments := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(arguments) == 0 {
		t.Fatal("ffmpeg received no arguments")
	}
	output := arguments[len(arguments)-1]
	if !strings.HasPrefix(filepath.Base(output), ".gopod-audio-") ||
		filepath.Ext(output) != ".part" {
		t.Fatalf("ffmpeg output %q is feed-eligible", output)
	}

	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == "-f" && arguments[index+1] == "ipod" {
			return
		}
	}
	t.Fatalf("ffmpeg arguments %q do not select the ipod muxer", arguments)
}

func waitForFFmpegPID(t *testing.T, path string) int {
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

func waitForFFmpegExit(t *testing.T, pid int) {
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
