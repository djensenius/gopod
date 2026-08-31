package podcast

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublishCompletedRecordingRejectsReservedShortTitle(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	renameCalled := false
	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: ".gopod-show", Directory: directory},
		audioStage,
		descriptionStage,
		time.Unix(123, 456),
		bytes.NewReader(bytes.Repeat([]byte{0x11}, 16)),
		func(_, _ string) error {
			renameCalled = true
			return errors.New("unexpected rename")
		},
	)
	if err == nil {
		t.Fatal("expected reserved short title error")
	}
	if !strings.Contains(err.Error(), `reserved prefix ".gopod-"`) {
		t.Fatalf("error %q does not report the reserved prefix", err)
	}
	if renameCalled {
		t.Fatal("publication attempted a rename for a reserved short title")
	}
	assertFileContent(t, audioStage, "audio")
	assertFileContent(t, descriptionStage, "description")
}

func TestPublishCompletedRecordingSkipsPreExistingOutputs(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("new audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("new description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(123, 456)
	firstToken := bytes.Repeat([]byte{0x11}, 16)
	secondToken := bytes.Repeat([]byte{0x22}, 16)
	firstStem := publicationStem("collision", recordedAt, firstToken)
	existingAudio := filepath.Join(directory, firstStem+".m4a")
	existingDescription := filepath.Join(directory, firstStem+".txt")
	if err := os.WriteFile(existingAudio, []byte("existing audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		existingDescription,
		[]byte("existing description"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	random := bytes.NewReader(append(
		append([]byte{}, firstToken...),
		secondToken...,
	))
	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "collision", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		random,
		renameNoReplace,
	)
	if err != nil {
		t.Fatal(err)
	}

	secondStem := publicationStem("collision", recordedAt, secondToken)
	if audioPath != filepath.Join(directory, secondStem+".m4a") {
		t.Fatalf("got audio path %q, want second unique stem", audioPath)
	}
	if descriptionPath != filepath.Join(directory, secondStem+".txt") {
		t.Fatalf("got description path %q, want matching unique stem", descriptionPath)
	}
	assertFileContent(t, existingAudio, "existing audio")
	assertFileContent(t, existingDescription, "existing description")
	assertFileContent(t, audioPath, "new audio")
	assertFileContent(t, descriptionPath, "new description")
	assertPathMissing(t, audioStage)
	assertPathMissing(t, descriptionStage)

	audioStem := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	descriptionStem := strings.TrimSuffix(
		filepath.Base(descriptionPath),
		filepath.Ext(descriptionPath),
	)
	if audioStem != descriptionStem {
		t.Fatalf("audio stem %q does not match description stem %q", audioStem, descriptionStem)
	}
}

func TestPublishCompletedRecordingUsesRenameStrategy(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	type renameCall struct {
		oldPath string
		newPath string
	}
	var calls []renameCall
	renamer := func(oldPath, newPath string) error {
		calls = append(calls, renameCall{oldPath: oldPath, newPath: newPath})
		return renameNoReplace(oldPath, newPath)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "rename", Directory: directory},
		audioStage,
		descriptionStage,
		time.Unix(456, 789),
		bytes.NewReader(bytes.Repeat([]byte{0x33}, 16)),
		renamer,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(calls) != 2 {
		t.Fatalf("got %d rename calls, want 2", len(calls))
	}
	if calls[0] != (renameCall{oldPath: descriptionStage, newPath: descriptionPath}) {
		t.Fatalf("first rename = %#v, want description publication", calls[0])
	}
	if calls[1] != (renameCall{oldPath: audioStage, newPath: audioPath}) {
		t.Fatalf("second rename = %#v, want audio publication", calls[1])
	}
	assertPathMissing(t, audioStage)
	assertPathMissing(t, descriptionStage)
	assertFileContent(t, audioPath, "audio")
	assertFileContent(t, descriptionPath, "description")
}

func TestRenameNoReplacePreservesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "existing.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := renameNoReplace(source, destination)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("got error %v, want destination-exists error", err)
	}
	assertFileContent(t, source, "source")
	assertFileContent(t, destination, "destination")
}

func TestPublishFailureRollsBackOnlyOwnedDescription(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingAudio := filepath.Join(directory, "existing.m4a")
	existingDescription := filepath.Join(directory, "existing.txt")
	if err := os.WriteFile(existingAudio, []byte("existing audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		existingDescription,
		[]byte("existing description"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(789, 123)
	token := bytes.Repeat([]byte{0x44}, 16)
	stem := publicationStem("failure", recordedAt, token)
	audioPath := filepath.Join(directory, stem+".m4a")
	descriptionPath := filepath.Join(directory, stem+".txt")
	injectedErr := errors.New("injected audio rename failure")
	renameCount := 0
	renamer := func(oldPath, newPath string) error {
		renameCount++
		if renameCount == 2 {
			return injectedErr
		}
		return renameNoReplace(oldPath, newPath)
	}

	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "failure", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(token),
		renamer,
	)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("got error %v, want injected rename failure", err)
	}

	assertPathMissing(t, audioPath)
	assertPathMissing(t, descriptionPath)
	assertFileContent(t, audioStage, "audio")
	assertPathMissing(t, descriptionStage)
	assertFileContent(t, existingAudio, "existing audio")
	assertFileContent(t, existingDescription, "existing description")
}

func TestPublishFailureDoesNotRemoveReplacedDescription(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(987, 654)
	token := bytes.Repeat([]byte{0x55}, 16)
	stem := publicationStem("replacement", recordedAt, token)
	descriptionPath := filepath.Join(directory, stem+".txt")
	displacedDescription := descriptionPath + ".displaced"
	injectedErr := errors.New("injected audio rename failure")
	renameCount := 0
	renamer := func(oldPath, newPath string) error {
		renameCount++
		if renameCount == 1 {
			return renameNoReplace(oldPath, newPath)
		}
		if err := os.Rename(descriptionPath, displacedDescription); err != nil {
			return err
		}
		if err := os.WriteFile(descriptionPath, []byte("replacement"), 0o644); err != nil {
			return err
		}
		return injectedErr
	}

	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "replacement", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(token),
		renamer,
	)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("got error %v, want injected rename failure", err)
	}
	if !strings.Contains(err.Error(), "refuse to remove published file") {
		t.Fatalf("error %q does not report ownership mismatch", err)
	}
	assertFileContent(t, descriptionPath, "replacement")
	assertFileContent(t, displacedDescription, "description")
	assertFileContent(t, audioStage, "audio")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("got %q in %q, want %q", data, path, want)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q still exists: %v", path, err)
	}
}
