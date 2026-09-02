package podcast

import (
	"bytes"
	"errors"
	"fmt"
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
	assertFileContent(t, descriptionStage, "new description")
	assertNoDescriptionPublicationStages(t, directory)

	audioStem := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	descriptionStem := strings.TrimSuffix(
		filepath.Base(descriptionPath),
		filepath.Ext(descriptionPath),
	)
	if audioStem != descriptionStem {
		t.Fatalf("audio stem %q does not match description stem %q", audioStem, descriptionStem)
	}
	assertCompletionMarker(t, directory, secondStem)
}

func TestPublishCompletedRecordingRetriesOwnershipCollision(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(223, 456)
	firstToken := bytes.Repeat([]byte{0x31}, publicationTokenSize)
	secondToken := bytes.Repeat([]byte{0x32}, publicationTokenSize)
	firstStem := publicationStem("ownership-race", recordedAt, firstToken)
	firstOwnership := publicationOwnershipPath(directory, firstStem)
	if err := os.WriteFile(
		firstOwnership,
		[]byte("another publisher"),
		publicationControlFileMode,
	); err != nil {
		t.Fatal(err)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "ownership-race", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(append(
			append([]byte{}, firstToken...),
			secondToken...,
		)),
		renameNoReplace,
	)
	if err != nil {
		t.Fatal(err)
	}

	secondStem := publicationStem("ownership-race", recordedAt, secondToken)
	if audioPath != filepath.Join(directory, secondStem+".m4a") ||
		descriptionPath != filepath.Join(directory, secondStem+".txt") {
		t.Fatalf(
			"got paths %q and %q, want stem %q",
			audioPath,
			descriptionPath,
			secondStem,
		)
	}
	assertFileContent(t, firstOwnership, "another publisher")
	assertPathMissing(t, filepath.Join(directory, firstStem+".m4a"))
	assertPathMissing(t, filepath.Join(directory, firstStem+".txt"))
	assertPathMissing(t, publicationCompletionMarkerPath(directory, firstStem))
	assertCompletionMarker(t, directory, secondStem)
}

func TestDelayedPublisherCannotReacquireCompletedStem(t *testing.T) {
	directory := t.TempDir()
	recordedAt := time.Unix(230, 456)
	token := bytes.Repeat([]byte{0x35}, publicationTokenSize)
	stem := publicationStem("delayed", recordedAt, token)
	paths := newPublicationPaths(directory, stem)

	firstAudioStage := filepath.Join(directory, ".gopod-audio-first.part")
	secondAudioStage := filepath.Join(directory, ".gopod-audio-second.part")
	secondDescriptionStage := filepath.Join(
		directory,
		".gopod-description-second.part",
	)
	for path, content := range map[string]string{
		firstAudioStage:        "first audio",
		secondAudioStage:       "second audio",
		secondDescriptionStage: "late description",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	unused, err := publicationPathsUnused(paths.all()...)
	if err != nil {
		t.Fatal(err)
	}
	if !unused {
		t.Fatal("second publisher's stale precheck unexpectedly found a collision")
	}
	secondAudioIdentity, err := os.Lstat(secondAudioStage)
	if err != nil {
		t.Fatal(err)
	}
	secondDescriptionIdentity, err := os.Lstat(secondDescriptionStage)
	if err != nil {
		t.Fatal(err)
	}

	firstAudioPath, firstDescriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "delayed", Directory: directory},
		firstAudioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		renameNoReplace,
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstAudioPath != paths.audio || firstDescriptionPath != "" {
		t.Fatalf(
			"first publication returned %q and %q, want %q and no description",
			firstAudioPath,
			firstDescriptionPath,
			paths.audio,
		)
	}

	renameCalled := false
	err = publishOwnedRecording(
		secondAudioStage,
		secondAudioIdentity,
		secondDescriptionStage,
		secondDescriptionIdentity,
		paths,
		newPublicationOperations(
			func(_, _ string) error {
				renameCalled = true
				return errors.New("unexpected second publication")
			},
		),
	)
	if !errors.Is(err, errPublicationCollision) {
		t.Fatalf("got delayed publisher error %v, want ownership collision", err)
	}
	if renameCalled {
		t.Fatal("delayed publisher rewrote a public output after completion")
	}
	assertFileContent(t, paths.audio, "first audio")
	assertPathMissing(t, paths.description)
	assertCompletionMarker(t, directory, stem)

	feed := generatePodcastFeedForTest(t, directory)
	if !strings.Contains(feed, filepath.Base(paths.audio)) {
		t.Fatalf("completed episode disappeared from the feed: %s", feed)
	}
}

func TestStaleClaimantCannotHideCompletedEpisode(t *testing.T) {
	directory := t.TempDir()
	recordedAt := time.Unix(231, 456)
	token := bytes.Repeat([]byte{0x36}, publicationTokenSize)
	stem := publicationStem("stale-claimant", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	audioStage := filepath.Join(directory, ".gopod-audio-first.part")
	if err := os.WriteFile(audioStage, []byte("completed audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	unused, err := publicationPathsUnused(paths.all()...)
	if err != nil {
		t.Fatal(err)
	}
	if !unused {
		t.Fatal("stale claimant's initial precheck unexpectedly found a collision")
	}
	if _, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "stale-claimant", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		renameNoReplace,
	); err != nil {
		t.Fatal(err)
	}
	ownershipBefore, err := os.ReadFile(paths.ownership)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := createPublicationOwnership(paths.ownership); !errors.Is(
		err,
		os.ErrExist,
	) {
		t.Fatalf("stale claimant acquired completed stem: %v", err)
	}
	ownershipAfter, err := os.ReadFile(paths.ownership)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ownershipBefore, ownershipAfter) {
		t.Fatal("stale claimant changed completed publication ownership")
	}
	assertPathMissing(t, paths.completionStage)
	assertCompletionMarker(t, directory, stem)

	feed := generatePodcastFeedForTest(t, directory)
	if !strings.Contains(feed, filepath.Base(paths.audio)) {
		t.Fatalf("stale claimant hid the completed episode: %s", feed)
	}
}

func TestPublishOwnedRecordingRechecksPathsAfterOwnership(t *testing.T) {
	directory := t.TempDir()
	recordedAt := time.Unix(232, 456)
	token := bytes.Repeat([]byte{0x37}, publicationTokenSize)
	stem := publicationStem("post-owner-recheck", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	if err := os.WriteFile(audioStage, []byte("owned audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	audioIdentity, err := os.Lstat(audioStage)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.audio, []byte("racing audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	renameCalled := false
	err = publishOwnedRecording(
		audioStage,
		audioIdentity,
		"",
		nil,
		paths,
		newPublicationOperations(
			func(_, _ string) error {
				renameCalled = true
				return errors.New("unexpected publication")
			},
		),
	)
	if !errors.Is(err, errPublicationCollision) {
		t.Fatalf("got error %v, want post-ownership collision", err)
	}
	if renameCalled {
		t.Fatal("publication ran after the post-ownership collision recheck")
	}
	assertFileContent(t, audioStage, "owned audio")
	assertFileContent(t, paths.audio, "racing audio")
	assertPathMissing(t, paths.ownership)
	assertPathMissing(t, paths.completionMarker)
	assertPathMissing(t, paths.completionStage)
}

func TestPublishCompletedRecordingRetriesAudioCollisionAfterPrecheck(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(234, 567)
	firstToken := bytes.Repeat([]byte{0x61}, publicationTokenSize)
	secondToken := bytes.Repeat([]byte{0x62}, publicationTokenSize)
	firstAudio := filepath.Join(
		directory,
		publicationStem("audio-race", recordedAt, firstToken)+".m4a",
	)
	secondAudio := filepath.Join(
		directory,
		publicationStem("audio-race", recordedAt, secondToken)+".m4a",
	)
	renamer := func(oldPath, newPath string) error {
		if newPath == firstAudio {
			if err := os.WriteFile(firstAudio, []byte("racing audio"), 0o644); err != nil {
				return err
			}
			return os.ErrExist
		}
		return renameNoReplace(oldPath, newPath)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "audio-race", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(append(
			append([]byte{}, firstToken...),
			secondToken...,
		)),
		renamer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audioPath != secondAudio {
		t.Fatalf("got audio path %q, want retry path %q", audioPath, secondAudio)
	}
	if descriptionPath != "" {
		t.Fatalf("got unexpected description path %q", descriptionPath)
	}
	assertFileContent(t, firstAudio, "racing audio")
	assertFileContent(t, secondAudio, "audio")
	assertPathMissing(t, audioStage)
	assertNoDescriptionPublicationStages(t, directory)
	assertPathMissing(
		t,
		publicationCompletionMarkerPath(
			directory,
			publicationStem("audio-race", recordedAt, firstToken),
		),
	)
	assertCompletionMarker(
		t,
		directory,
		publicationStem("audio-race", recordedAt, secondToken),
	)
	assertPathMissing(
		t,
		publicationOwnershipPath(
			directory,
			publicationStem("audio-race", recordedAt, firstToken),
		),
	)
}

func TestPublishCompletedRecordingRetriesFromCanonicalDescriptionStage(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(345, 678)
	firstToken := bytes.Repeat([]byte{0x63}, publicationTokenSize)
	secondToken := bytes.Repeat([]byte{0x64}, publicationTokenSize)
	firstStem := publicationStem("description-race", recordedAt, firstToken)
	firstAudio := filepath.Join(directory, firstStem+".m4a")
	firstDescription := filepath.Join(directory, firstStem+".txt")
	secondStem := publicationStem("description-race", recordedAt, secondToken)
	secondAudio := filepath.Join(directory, secondStem+".m4a")
	secondDescription := filepath.Join(directory, secondStem+".txt")
	var descriptionSources []string
	renamer := func(oldPath, newPath string) error {
		if strings.HasSuffix(newPath, ".txt") {
			descriptionSources = append(descriptionSources, oldPath)
		}
		if oldPath == audioStage && newPath == firstAudio {
			if err := os.WriteFile(firstAudio, []byte("racing audio"), 0o644); err != nil {
				return err
			}
			return os.ErrExist
		}
		return renameNoReplace(oldPath, newPath)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "description-race", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(append(
			append([]byte{}, firstToken...),
			secondToken...,
		)),
		renamer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptionSources) != 2 {
		t.Fatalf(
			"got %d description publication sources, want one per attempt",
			len(descriptionSources),
		)
	}
	if descriptionSources[0] == descriptionStage ||
		descriptionSources[1] == descriptionStage {
		t.Fatal("canonical description stage was published directly")
	}
	if descriptionSources[0] == descriptionSources[1] {
		t.Fatal("retry reused the prior disposable description stage")
	}
	if audioPath != secondAudio || descriptionPath != secondDescription {
		t.Fatalf(
			"got paths %q and %q, want %q and %q",
			audioPath,
			descriptionPath,
			secondAudio,
			secondDescription,
		)
	}
	assertFileContent(t, firstAudio, "racing audio")
	assertFileContent(t, firstDescription, "description")
	assertFileContent(t, secondAudio, "audio")
	assertFileContent(t, secondDescription, "description")
	assertPathMissing(t, audioStage)
	assertFileContent(t, descriptionStage, "description")
	for _, source := range descriptionSources {
		assertPathMissing(t, source)
	}
	assertNoDescriptionPublicationStages(t, directory)
	assertPathMissing(
		t,
		publicationCompletionMarkerPath(directory, firstStem),
	)
	assertCompletionMarker(t, directory, secondStem)
	if _, err := os.Lstat(publicationOwnershipPath(directory, firstStem)); err != nil {
		t.Fatalf("partial publication ownership is missing: %v", err)
	}

	feed := generatePodcastFeedForTest(t, directory)
	if strings.Contains(feed, filepath.Base(firstAudio)) {
		t.Fatalf("feed contains uncommitted racing audio %q", firstAudio)
	}
	if !strings.Contains(feed, filepath.Base(secondAudio)) {
		t.Fatalf("feed does not contain committed retry audio %q", secondAudio)
	}
	if strings.Count(feed, "<item>") != 1 {
		t.Fatalf("feed contains an unexpected number of items: %s", feed)
	}
}

func TestPublishCompletedRecordingRetryDoesNotTouchReplacedPublicDescription(
	t *testing.T,
) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(456, 789)
	firstToken := bytes.Repeat([]byte{0x65}, publicationTokenSize)
	secondToken := bytes.Repeat([]byte{0x66}, publicationTokenSize)
	firstStem := publicationStem("public-replacement", recordedAt, firstToken)
	firstAudio := filepath.Join(directory, firstStem+".m4a")
	firstDescription := filepath.Join(directory, firstStem+".txt")
	displacedDescription := firstDescription + ".owned"
	secondStem := publicationStem("public-replacement", recordedAt, secondToken)
	secondAudio := filepath.Join(directory, secondStem+".m4a")
	secondDescription := filepath.Join(directory, secondStem+".txt")
	publicDescriptionUsedAsSource := false
	renamer := func(oldPath, newPath string) error {
		if oldPath == firstDescription {
			publicDescriptionUsedAsSource = true
			return errors.New("public description used as a rename source")
		}
		if oldPath == audioStage && newPath == firstAudio {
			if err := os.Rename(firstDescription, displacedDescription); err != nil {
				return err
			}
			if err := os.WriteFile(
				firstDescription,
				[]byte("replacement description"),
				0o644,
			); err != nil {
				return err
			}
			if err := os.WriteFile(firstAudio, []byte("racing audio"), 0o644); err != nil {
				return err
			}
			return os.ErrExist
		}
		return renameNoReplace(oldPath, newPath)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "public-replacement", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(append(
			append([]byte{}, firstToken...),
			secondToken...,
		)),
		renamer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if publicDescriptionUsedAsSource {
		t.Fatal("retry attempted to reclaim the replaced public description")
	}
	if audioPath != secondAudio || descriptionPath != secondDescription {
		t.Fatalf(
			"got paths %q and %q, want %q and %q",
			audioPath,
			descriptionPath,
			secondAudio,
			secondDescription,
		)
	}
	assertFileContent(t, firstDescription, "replacement description")
	assertFileContent(t, displacedDescription, "description")
	assertFileContent(t, firstAudio, "racing audio")
	assertFileContent(t, secondAudio, "audio")
	assertFileContent(t, secondDescription, "description")
	assertFileContent(t, descriptionStage, "description")
	assertNoDescriptionPublicationStages(t, directory)
}

func TestPublishCompletedRecordingDetectsReplacedDescriptionDuringLinkRemove(
	t *testing.T,
) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(567, 890)
	token := bytes.Repeat([]byte{0x67}, publicationTokenSize)
	stem := publicationStem("link-remove", recordedAt, token)
	audioPath := filepath.Join(directory, stem+".m4a")
	descriptionPath := filepath.Join(directory, stem+".txt")
	displacedDescription := descriptionPath + ".linked"
	linkRemoveInjected := false
	renamer := func(oldPath, newPath string) error {
		if newPath == descriptionPath {
			if oldPath == descriptionStage {
				return errors.New("canonical description stage used by link fallback")
			}
			if err := os.Link(oldPath, newPath); err != nil {
				return err
			}
			if err := os.Rename(newPath, displacedDescription); err != nil {
				return err
			}
			if err := os.WriteFile(
				newPath,
				[]byte("replacement during link/remove"),
				0o644,
			); err != nil {
				return err
			}
			if err := os.Remove(oldPath); err != nil {
				return err
			}
			linkRemoveInjected = true
			return nil
		}
		if newPath == audioPath {
			return errors.New("audio publication ran after description ownership changed")
		}
		return renameNoReplace(oldPath, newPath)
	}

	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "link-remove", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(token),
		renamer,
	)
	if err == nil {
		t.Fatal("expected replaced description ownership error")
	}
	if !strings.Contains(err.Error(), "published podcast description") ||
		!strings.Contains(err.Error(), "ownership changed") {
		t.Fatalf("error %q does not report description ownership change", err)
	}
	if !linkRemoveInjected {
		t.Fatal("Linux-like link/remove publication sequence was not injected")
	}
	assertFileContent(t, descriptionPath, "replacement during link/remove")
	assertFileContent(t, displacedDescription, "description")
	assertPathMissing(t, audioPath)
	assertFileContent(t, descriptionStage, "description")
	assertPathMissing(t, publicationCompletionMarkerPath(directory, stem))
	if _, statErr := os.Lstat(
		publicationOwnershipPath(directory, stem),
	); statErr != nil {
		t.Fatalf("partial publication ownership is missing: %v", statErr)
	}
	assertNoDescriptionPublicationStages(t, directory)
}

func TestPublishCompletedRecordingDetectsSwappedPublicationSource(
	t *testing.T,
) {
	t.Run("audio", func(t *testing.T) {
		directory := t.TempDir()
		audioStage := filepath.Join(directory, ".gopod-audio-test.part")
		if err := os.WriteFile(audioStage, []byte("owned audio"), 0o600); err != nil {
			t.Fatal(err)
		}

		recordedAt := time.Unix(568, 890)
		token := bytes.Repeat([]byte{0x69}, publicationTokenSize)
		stem := publicationStem("audio-source-swap", recordedAt, token)
		paths := newPublicationPaths(directory, stem)
		displacedAudioStage := audioStage + ".owned"
		renamer := func(oldPath, newPath string) error {
			if newPath == paths.audio {
				if err := os.Rename(oldPath, displacedAudioStage); err != nil {
					return err
				}
				if err := os.WriteFile(
					oldPath,
					[]byte("replacement audio"),
					0o600,
				); err != nil {
					return err
				}
			}
			return renameNoReplace(oldPath, newPath)
		}

		_, _, err := publishCompletedRecordingWith(
			Podcast{ShortTitle: "audio-source-swap", Directory: directory},
			audioStage,
			"",
			recordedAt,
			bytes.NewReader(token),
			renamer,
		)
		if err == nil {
			t.Fatal("expected swapped audio source error")
		}
		if !strings.Contains(err.Error(), "published podcast audio") ||
			!strings.Contains(err.Error(), "ownership changed") {
			t.Fatalf("error %q does not report swapped audio ownership", err)
		}
		assertFileContent(t, displacedAudioStage, "owned audio")
		assertFileContent(t, paths.audio, "replacement audio")
		assertPathMissing(t, paths.completionMarker)
		assertPathMissing(t, paths.completionStage)
		if _, statErr := os.Lstat(paths.ownership); statErr != nil {
			t.Fatalf("partial publication ownership is missing: %v", statErr)
		}
		feed := generatePodcastFeedForTest(t, directory)
		if strings.Contains(feed, filepath.Base(paths.audio)) {
			t.Fatalf("feed contains audio published from a swapped source: %s", feed)
		}
	})

	t.Run("description", func(t *testing.T) {
		directory := t.TempDir()
		audioStage := filepath.Join(directory, ".gopod-audio-test.part")
		descriptionStage := filepath.Join(
			directory,
			".gopod-description-test.part",
		)
		if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			descriptionStage,
			[]byte("owned description"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}

		recordedAt := time.Unix(569, 890)
		token := bytes.Repeat([]byte{0x6a}, publicationTokenSize)
		stem := publicationStem("description-source-swap", recordedAt, token)
		paths := newPublicationPaths(directory, stem)
		displacedDescriptionStage := ""
		renamer := func(oldPath, newPath string) error {
			if newPath == paths.description {
				displacedDescriptionStage = oldPath + ".owned"
				if err := os.Rename(oldPath, displacedDescriptionStage); err != nil {
					return err
				}
				if err := os.WriteFile(
					oldPath,
					[]byte("replacement description"),
					0o600,
				); err != nil {
					return err
				}
			}
			return renameNoReplace(oldPath, newPath)
		}

		_, _, err := publishCompletedRecordingWith(
			Podcast{
				ShortTitle: "description-source-swap",
				Directory:  directory,
			},
			audioStage,
			descriptionStage,
			recordedAt,
			bytes.NewReader(token),
			renamer,
		)
		if err == nil {
			t.Fatal("expected swapped description source error")
		}
		if !strings.Contains(err.Error(), "published podcast description") ||
			!strings.Contains(err.Error(), "ownership changed") {
			t.Fatalf("error %q does not report swapped description ownership", err)
		}
		if displacedDescriptionStage == "" {
			t.Fatal("description publication source was not swapped")
		}
		assertFileContent(
			t,
			displacedDescriptionStage,
			"owned description",
		)
		assertFileContent(t, paths.description, "replacement description")
		assertFileContent(t, audioStage, "audio")
		assertFileContent(t, descriptionStage, "owned description")
		assertPathMissing(t, paths.audio)
		assertPathMissing(t, paths.completionMarker)
		assertPathMissing(t, paths.completionStage)
		if _, statErr := os.Lstat(paths.ownership); statErr != nil {
			t.Fatalf("partial publication ownership is missing: %v", statErr)
		}
	})
}

func TestPublishCompletedRecordingCollisionRequiresReusableCanonicalStages(
	t *testing.T,
) {
	t.Run("description ownership changes", func(t *testing.T) {
		directory := t.TempDir()
		audioStage := filepath.Join(directory, ".gopod-audio-test.part")
		descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
		if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
			t.Fatal(err)
		}

		recordedAt := time.Unix(678, 901)
		firstToken := bytes.Repeat([]byte{0x69}, publicationTokenSize)
		secondToken := bytes.Repeat([]byte{0x6a}, publicationTokenSize)
		firstStem := publicationStem("description-owner", recordedAt, firstToken)
		firstAudio := filepath.Join(directory, firstStem+".m4a")
		firstDescription := filepath.Join(directory, firstStem+".txt")
		secondStem := publicationStem("description-owner", recordedAt, secondToken)
		secondAudio := filepath.Join(directory, secondStem+".m4a")
		displacedStage := descriptionStage + ".owned"
		renamer := func(oldPath, newPath string) error {
			if oldPath == audioStage && newPath == firstAudio {
				if err := os.Rename(descriptionStage, displacedStage); err != nil {
					return err
				}
				if err := os.WriteFile(
					descriptionStage,
					[]byte("replacement canonical description"),
					0o600,
				); err != nil {
					return err
				}
				if err := os.WriteFile(firstAudio, []byte("racing audio"), 0o644); err != nil {
					return err
				}
				return os.ErrExist
			}
			if newPath == secondAudio {
				return errors.New("unexpected retry")
			}
			return renameNoReplace(oldPath, newPath)
		}

		_, _, err := publishCompletedRecordingWith(
			Podcast{ShortTitle: "description-owner", Directory: directory},
			audioStage,
			descriptionStage,
			recordedAt,
			bytes.NewReader(append(
				append([]byte{}, firstToken...),
				secondToken...,
			)),
			renamer,
		)
		if err == nil {
			t.Fatal("expected canonical description ownership error")
		}
		if errors.Is(err, errPublicationCollision) {
			t.Fatalf("ownership failure was marked retryable: %v", err)
		}
		if !strings.Contains(err.Error(), "description staging file") ||
			!strings.Contains(err.Error(), "ownership changed") {
			t.Fatalf("error %q does not report description ownership change", err)
		}
		assertFileContent(t, audioStage, "audio")
		assertFileContent(t, descriptionStage, "replacement canonical description")
		assertFileContent(t, displacedStage, "description")
		assertFileContent(t, firstDescription, "description")
		assertFileContent(t, firstAudio, "racing audio")
		assertPathMissing(t, secondAudio)
		assertNoDescriptionPublicationStages(t, directory)
	})

	t.Run("audio ownership changes", func(t *testing.T) {
		directory := t.TempDir()
		audioStage := filepath.Join(directory, ".gopod-audio-test.part")
		descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
		if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
			t.Fatal(err)
		}

		recordedAt := time.Unix(789, 12)
		firstToken := bytes.Repeat([]byte{0x6b}, publicationTokenSize)
		secondToken := bytes.Repeat([]byte{0x6c}, publicationTokenSize)
		firstStem := publicationStem("audio-owner", recordedAt, firstToken)
		firstAudio := filepath.Join(directory, firstStem+".m4a")
		firstDescription := filepath.Join(directory, firstStem+".txt")
		secondStem := publicationStem("audio-owner", recordedAt, secondToken)
		secondAudio := filepath.Join(directory, secondStem+".m4a")
		displacedStage := audioStage + ".owned"
		renamer := func(oldPath, newPath string) error {
			if oldPath == audioStage && newPath == firstAudio {
				if err := os.Rename(audioStage, displacedStage); err != nil {
					return err
				}
				if err := os.WriteFile(
					audioStage,
					[]byte("replacement canonical audio"),
					0o600,
				); err != nil {
					return err
				}
				if err := os.WriteFile(firstAudio, []byte("racing audio"), 0o644); err != nil {
					return err
				}
				return os.ErrExist
			}
			if newPath == secondAudio {
				return errors.New("unexpected retry")
			}
			return renameNoReplace(oldPath, newPath)
		}

		_, _, err := publishCompletedRecordingWith(
			Podcast{ShortTitle: "audio-owner", Directory: directory},
			audioStage,
			descriptionStage,
			recordedAt,
			bytes.NewReader(append(
				append([]byte{}, firstToken...),
				secondToken...,
			)),
			renamer,
		)
		if err == nil {
			t.Fatal("expected canonical audio ownership error")
		}
		if errors.Is(err, errPublicationCollision) {
			t.Fatalf("ownership failure was marked retryable: %v", err)
		}
		if !strings.Contains(err.Error(), "audio staging file") ||
			!strings.Contains(err.Error(), "ownership changed") {
			t.Fatalf("error %q does not report audio ownership change", err)
		}
		assertFileContent(t, audioStage, "replacement canonical audio")
		assertFileContent(t, displacedStage, "audio")
		assertFileContent(t, descriptionStage, "description")
		assertFileContent(t, firstDescription, "description")
		assertFileContent(t, firstAudio, "racing audio")
		assertPathMissing(t, secondAudio)
		assertNoDescriptionPublicationStages(t, directory)
	})
}

func TestPublishCompletedRecordingDescriptionCopyFailureDoesNotRetry(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(descriptionStage, 0o700); err != nil {
		t.Fatal(err)
	}

	renameCalled := false
	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "copy-failure", Directory: directory},
		audioStage,
		descriptionStage,
		time.Unix(890, 123),
		bytes.NewReader(bytes.Repeat(
			[]byte{0x6d},
			publicationTokenSize*2,
		)),
		func(_, _ string) error {
			renameCalled = true
			return errors.New("unexpected publication")
		},
	)
	if err == nil {
		t.Fatal("expected description copy failure")
	}
	if errors.Is(err, errPublicationCollision) {
		t.Fatalf("copy failure was marked retryable: %v", err)
	}
	if renameCalled {
		t.Fatal("publication ran after the description copy failed")
	}
	assertFileContent(t, audioStage, "audio")
	if info, statErr := os.Stat(descriptionStage); statErr != nil || !info.IsDir() {
		t.Fatalf("canonical description directory changed: %v", statErr)
	}
	assertNoDescriptionPublicationStages(t, directory)
	assertNoPublicationOwnerships(t, directory)
}

func TestPublishCompletedRecordingExhaustedCollisionsPreserveStages(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	descriptionStage := filepath.Join(directory, ".gopod-description-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptionStage, []byte("description"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(678, 901)
	randomData := make([]byte, 0, publicationMaxAttempts*publicationTokenSize)
	var firstAudio string
	var firstDescription string
	var lastAudio string
	var lastDescription string
	for attempt := 0; attempt < publicationMaxAttempts; attempt++ {
		token := bytes.Repeat(
			[]byte{byte(attempt + 1)},
			publicationTokenSize,
		)
		randomData = append(randomData, token...)
		stem := publicationStem("exhausted", recordedAt, token)
		audioPath := filepath.Join(directory, stem+".m4a")
		descriptionPath := filepath.Join(directory, stem+".txt")
		if attempt == 0 {
			firstAudio = audioPath
			firstDescription = descriptionPath
		}
		lastAudio = audioPath
		lastDescription = descriptionPath
	}
	renamer := func(oldPath, newPath string) error {
		if oldPath == audioStage {
			if err := os.WriteFile(newPath, []byte("racing audio"), 0o644); err != nil {
				return err
			}
			return os.ErrExist
		}
		return renameNoReplace(oldPath, newPath)
	}

	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "exhausted", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(randomData),
		renamer,
	)
	if err == nil {
		t.Fatal("expected exhausted collision error")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("error %q does not report collision exhaustion", err)
	}
	assertFileContent(t, audioStage, "audio")
	assertFileContent(t, descriptionStage, "description")
	assertFileContent(t, firstAudio, "racing audio")
	assertFileContent(t, firstDescription, "description")
	assertFileContent(t, lastAudio, "racing audio")
	assertFileContent(t, lastDescription, "description")
	descriptionOutputs, globErr := filepath.Glob(
		filepath.Join(directory, "exhausted-*.txt"),
	)
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(descriptionOutputs) != publicationMaxAttempts {
		t.Fatalf(
			"got %d abandoned descriptions, want %d",
			len(descriptionOutputs),
			publicationMaxAttempts,
		)
	}
	assertNoDescriptionPublicationStages(t, directory)
	assertPublicationOwnershipCount(t, directory, publicationMaxAttempts)
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
	recordedAt := time.Unix(456, 789)
	token := bytes.Repeat([]byte{0x33}, publicationTokenSize)
	stem := publicationStem("rename", recordedAt, token)
	expectedAudioPath := filepath.Join(directory, stem+".m4a")
	expectedDescriptionPath := filepath.Join(directory, stem+".txt")
	ownershipPath := publicationOwnershipPath(directory, stem)
	completionMarkerPath := publicationCompletionMarkerPath(directory, stem)
	completionStagePath := publicationCompletionStagePath(directory, stem)
	visibilityGuardPath := publicationVisibilityGuardPath(directory, stem)
	visibilityMarkerPath := publicationVisibilityMarkerPath(directory, stem)
	var calls []renameCall
	renamer := func(oldPath, newPath string) error {
		calls = append(calls, renameCall{oldPath: oldPath, newPath: newPath})
		if newPath == completionMarkerPath {
			assertFileContent(t, expectedAudioPath, "audio")
			assertFileContent(t, expectedDescriptionPath, "description")
			if _, err := os.Lstat(visibilityGuardPath); err != nil {
				t.Fatalf("visibility guard is missing during marker rename: %v", err)
			}
			committed, err := hasPublicationCompletionMarker(directory, stem)
			if err != nil {
				t.Fatal(err)
			}
			if committed {
				t.Fatal("publication became feed-visible during marker rename")
			}
		}
		return renameNoReplace(oldPath, newPath)
	}

	audioPath, descriptionPath, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "rename", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(token),
		renamer,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(calls) != 4 {
		t.Fatalf("got %d rename calls, want 4", len(calls))
	}
	if calls[0].newPath != descriptionPath {
		t.Fatalf("first rename = %#v, want description publication", calls[0])
	}
	if calls[0].oldPath == descriptionStage {
		t.Fatal("description publication used the canonical staging file")
	}
	if !strings.HasPrefix(
		filepath.Base(calls[0].oldPath),
		".gopod-description-publish-",
	) {
		t.Fatalf(
			"description publication source %q is not a disposable stage",
			calls[0].oldPath,
		)
	}
	if calls[1] != (renameCall{oldPath: audioStage, newPath: audioPath}) {
		t.Fatalf("second rename = %#v, want audio publication", calls[1])
	}
	if calls[2] != (renameCall{
		oldPath: completionStagePath,
		newPath: completionMarkerPath,
	}) {
		t.Fatalf("third rename = %#v, want completion marker commit", calls[2])
	}
	if calls[3] != (renameCall{
		oldPath: visibilityGuardPath,
		newPath: visibilityMarkerPath,
	}) {
		t.Fatalf("fourth rename = %#v, want visibility marker commit", calls[3])
	}
	if _, err := os.Lstat(ownershipPath); err != nil {
		t.Fatalf("persistent publication ownership is missing: %v", err)
	}
	assertPathMissing(t, audioStage)
	assertFileContent(t, descriptionStage, "description")
	assertPathMissing(t, calls[0].oldPath)
	assertFileContent(t, audioPath, "audio")
	assertFileContent(t, descriptionPath, "description")
	assertCompletionMarker(t, directory, stem)
	assertPathMissing(t, visibilityGuardPath)
	assertNoDescriptionPublicationStages(t, directory)
}

func TestPublicationCommitDurabilityOrdering(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-order.part")
	descriptionStage := filepath.Join(
		directory,
		".gopod-description-order.part",
	)
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		descriptionStage,
		[]byte("description"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(600, 123)
	token := bytes.Repeat([]byte{0x81}, publicationTokenSize)
	stem := publicationStem("durability-order", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	operations := newPublicationOperations(renameNoReplace)
	var events []string

	syncPublishedFile := operations.syncPublishedFile
	operations.syncPublishedFile = func(
		kind string,
		path string,
		expectedIdentity os.FileInfo,
	) error {
		events = append(events, "sync-file:"+kind)
		return syncPublishedFile(kind, path, expectedIdentity)
	}
	syncDirectory := operations.syncDirectory
	directorySyncs := 0
	operations.syncDirectory = func(path string) error {
		directorySyncs++
		switch directorySyncs {
		case 1:
			events = append(events, "sync-directory:prepared")
			if _, err := os.Lstat(paths.visibilityGuard); err != nil {
				t.Fatalf("prepared visibility guard is missing: %v", err)
			}
			if _, err := os.Lstat(paths.completionStage); err != nil {
				t.Fatalf("prepared completion stage is missing: %v", err)
			}
			assertPathMissing(t, paths.completionMarker)
		case 2:
			events = append(events, "sync-directory:marker")
			if _, err := os.Lstat(paths.visibilityGuard); err != nil {
				t.Fatalf("marker is not guarded before its directory sync: %v", err)
			}
			assertPathMissing(t, paths.completionStage)
			if _, err := os.Lstat(paths.completionMarker); err != nil {
				t.Fatalf("completion marker is missing before directory sync: %v", err)
			}
		case 3:
			events = append(events, "sync-directory:visible")
			assertPathMissing(t, paths.visibilityGuard)
			if _, err := os.Lstat(paths.completionMarker); err != nil {
				t.Fatalf("visible completion marker is missing: %v", err)
			}
			if _, err := os.Lstat(paths.visibilityMarker); err != nil {
				t.Fatalf("visibility marker is missing: %v", err)
			}
		default:
			t.Fatalf("unexpected publication directory sync %d", directorySyncs)
		}
		return syncDirectory(path)
	}
	operations.renameFile = func(oldPath, newPath string) error {
		if newPath == paths.completionMarker {
			events = append(events, "rename-marker")
			if _, err := os.Lstat(paths.visibilityGuard); err != nil {
				t.Fatalf("visibility guard is missing during marker rename: %v", err)
			}
		}
		return renameNoReplace(oldPath, newPath)
	}
	publishVisibility := operations.publishVisibility
	operations.publishVisibility = func(
		oldPath string,
		newPath string,
		expectedIdentity os.FileInfo,
	) error {
		events = append(events, "rename-visibility")
		if oldPath != paths.visibilityGuard ||
			newPath != paths.visibilityMarker {
			t.Fatalf(
				"visibility move = %q -> %q, want %q -> %q",
				oldPath,
				newPath,
				paths.visibilityGuard,
				paths.visibilityMarker,
			)
		}
		return publishVisibility(oldPath, newPath, expectedIdentity)
	}

	if _, _, err := publishCompletedRecordingWithOperations(
		Podcast{ShortTitle: "durability-order", Directory: directory},
		audioStage,
		descriptionStage,
		recordedAt,
		bytes.NewReader(token),
		operations,
	); err != nil {
		t.Fatal(err)
	}

	wantEvents := []string{
		"sync-file:audio",
		"sync-file:description",
		"sync-directory:prepared",
		"rename-marker",
		"sync-directory:marker",
		"rename-visibility",
		"sync-directory:visible",
	}
	if strings.Join(events, "\n") != strings.Join(wantEvents, "\n") {
		t.Fatalf("got publication ordering %v, want %v", events, wantEvents)
	}
	assertCompletionMarker(t, directory, stem)
}

func TestPublicationVisibilityCommitLostReplyCompletes(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-guard-failure.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(601, 123)
	token := bytes.Repeat([]byte{0x82}, publicationTokenSize)
	stem := publicationStem("guard-failure", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	operations := newPublicationOperations(renameNoReplace)
	syncDirectory := operations.syncDirectory
	directorySyncs := 0
	operations.syncDirectory = func(path string) error {
		directorySyncs++
		return syncDirectory(path)
	}
	injectedErr := errors.New("injected visibility commit lost reply")
	publishVisibility := operations.publishVisibility
	operations.publishVisibility = func(
		oldPath string,
		newPath string,
		expectedIdentity os.FileInfo,
	) error {
		if oldPath != paths.visibilityGuard ||
			newPath != paths.visibilityMarker {
			t.Fatalf(
				"visibility move = %q -> %q, want %q -> %q",
				oldPath,
				newPath,
				paths.visibilityGuard,
				paths.visibilityMarker,
			)
		}
		if err := publishVisibility(
			oldPath,
			newPath,
			expectedIdentity,
		); err != nil {
			return err
		}
		return injectedErr
	}

	_, _, err := publishCompletedRecordingWithOperations(
		Podcast{ShortTitle: "guard-failure", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		operations,
	)
	if err != nil {
		t.Fatalf("lost visibility reply did not resolve as committed: %v", err)
	}
	if directorySyncs != 3 {
		t.Fatalf("got %d directory syncs, want final sync", directorySyncs)
	}
	assertFileContent(t, paths.audio, "audio")
	assertPathMissing(t, paths.visibilityGuard)
	if _, err := os.Lstat(paths.visibilityMarker); err != nil {
		t.Fatalf("lost visibility reply did not leave its marker: %v", err)
	}
	if _, err := os.Lstat(paths.completionMarker); err != nil {
		t.Fatalf("failed visibility commit removed completion marker: %v", err)
	}
	assertPathMissing(t, paths.completionStage)
	committed, markerErr := hasPublicationCompletionMarker(directory, stem)
	if markerErr != nil {
		t.Fatal(markerErr)
	}
	if !committed {
		t.Fatal("completed visibility move was not feed-visible")
	}
}

func TestPublicationVisibilityCommitFailureStaysInvisible(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-guard-failure.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(601, 124)
	token := bytes.Repeat([]byte{0x84}, publicationTokenSize)
	stem := publicationStem("guard-failure", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	operations := newPublicationOperations(renameNoReplace)
	syncDirectory := operations.syncDirectory
	directorySyncs := 0
	operations.syncDirectory = func(path string) error {
		directorySyncs++
		return syncDirectory(path)
	}
	injectedErr := errors.New("injected visibility commit failure")
	operations.publishVisibility = func(
		oldPath string,
		newPath string,
		_ os.FileInfo,
	) error {
		if oldPath != paths.visibilityGuard ||
			newPath != paths.visibilityMarker {
			t.Fatalf(
				"visibility move = %q -> %q, want %q -> %q",
				oldPath,
				newPath,
				paths.visibilityGuard,
				paths.visibilityMarker,
			)
		}
		return injectedErr
	}

	_, _, err := publishCompletedRecordingWithOperations(
		Podcast{ShortTitle: "guard-failure", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		operations,
	)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("got error %v, want visibility commit failure", err)
	}
	if directorySyncs != 2 {
		t.Fatalf("got %d directory syncs, want no final sync", directorySyncs)
	}
	assertFileContent(t, paths.audio, "audio")
	if _, err := os.Lstat(paths.visibilityGuard); err != nil {
		t.Fatalf("failed visibility commit removed its guard: %v", err)
	}
	assertPathMissing(t, paths.visibilityMarker)
	if _, err := os.Lstat(paths.completionMarker); err != nil {
		t.Fatalf("failed visibility commit removed completion marker: %v", err)
	}
	assertPathMissing(t, paths.completionStage)
	committed, markerErr := hasPublicationCompletionMarker(directory, stem)
	if markerErr != nil {
		t.Fatal(markerErr)
	}
	if committed {
		t.Fatal("failed visibility commit left the publication feed-visible")
	}
}

func TestPublicationFinalDirectorySyncFailureIsPostCommit(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-final-sync.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(602, 123)
	token := bytes.Repeat([]byte{0x83}, publicationTokenSize)
	stem := publicationStem("final-sync", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	operations := newPublicationOperations(renameNoReplace)
	syncDirectory := operations.syncDirectory
	directorySyncs := 0
	injectedErr := errors.New("injected final directory sync failure")
	operations.syncDirectory = func(path string) error {
		directorySyncs++
		if directorySyncs == 3 {
			return injectedErr
		}
		return syncDirectory(path)
	}

	_, _, err := publishCompletedRecordingWithOperations(
		Podcast{ShortTitle: "final-sync", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		operations,
	)
	if !errors.Is(err, injectedErr) ||
		!strings.Contains(err.Error(), "post-commit durability error") {
		t.Fatalf("got error %v, want explicit post-commit durability failure", err)
	}
	if directorySyncs != 3 {
		t.Fatalf("got %d directory syncs, want final sync attempt", directorySyncs)
	}
	assertFileContent(t, paths.audio, "audio")
	assertPathMissing(t, paths.visibilityGuard)
	assertPathMissing(t, paths.completionStage)
	if _, err := os.Lstat(paths.completionMarker); err != nil {
		t.Fatalf("post-commit sync failure removed completion marker: %v", err)
	}
	if _, err := os.Lstat(paths.ownership); err != nil {
		t.Fatalf("post-commit sync failure removed ownership: %v", err)
	}
	if _, err := os.Lstat(paths.visibilityMarker); err != nil {
		t.Fatalf("post-commit sync failure removed visibility marker: %v", err)
	}
	committed, markerErr := hasPublicationCompletionMarker(directory, stem)
	if markerErr != nil {
		t.Fatal(markerErr)
	}
	if !committed {
		t.Fatal("final sync failure recreated an invisibility control")
	}
}

func TestPublishCompletedRecordingMarkerFailureLeavesOutputsUncommitted(
	t *testing.T,
) {
	injectedFailure := errors.New("injected marker failure")
	tests := []struct {
		name                  string
		markerError           error
		createCollidingMarker bool
	}{
		{
			name:                  "collision",
			markerError:           os.ErrExist,
			createCollidingMarker: true,
		},
		{
			name:        "failure",
			markerError: injectedFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			audioStage := filepath.Join(directory, ".gopod-audio-test.part")
			descriptionStage := filepath.Join(
				directory,
				".gopod-description-test.part",
			)
			if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				descriptionStage,
				[]byte("uncommitted description"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			recordedAt := time.Unix(567, 890)
			token := bytes.Repeat([]byte{0x73}, publicationTokenSize)
			stem := publicationStem("marker-failure", recordedAt, token)
			audioPath := filepath.Join(directory, stem+".m4a")
			descriptionPath := filepath.Join(directory, stem+".txt")
			ownershipPath := publicationOwnershipPath(directory, stem)
			completionMarkerPath := publicationCompletionMarkerPath(
				directory,
				stem,
			)
			completionStagePath := publicationCompletionStagePath(
				directory,
				stem,
			)
			renamer := func(oldPath, newPath string) error {
				if newPath == completionMarkerPath {
					assertFileContent(t, audioPath, "audio")
					assertFileContent(
						t,
						descriptionPath,
						"uncommitted description",
					)
					if test.createCollidingMarker {
						if err := os.WriteFile(
							completionMarkerPath,
							[]byte("another publisher"),
							publicationControlFileMode,
						); err != nil {
							return err
						}
					}
					return test.markerError
				}
				return renameNoReplace(oldPath, newPath)
			}

			_, _, err := publishCompletedRecordingWith(
				Podcast{ShortTitle: "marker-failure", Directory: directory},
				audioStage,
				descriptionStage,
				recordedAt,
				bytes.NewReader(token),
				renamer,
			)
			if !errors.Is(err, test.markerError) {
				t.Fatalf("got error %v, want %v", err, test.markerError)
			}

			assertFileContent(t, audioPath, "audio")
			assertFileContent(
				t,
				descriptionPath,
				"uncommitted description",
			)
			assertPathMissing(t, audioStage)
			assertFileContent(
				t,
				descriptionStage,
				"uncommitted description",
			)
			if _, err := os.Lstat(ownershipPath); err != nil {
				t.Fatalf("publication ownership was not preserved: %v", err)
			}
			if _, err := os.Lstat(completionStagePath); err != nil {
				t.Fatalf("publication commit guard was not preserved: %v", err)
			}
			if test.createCollidingMarker {
				assertFileContent(
					t,
					completionMarkerPath,
					"another publisher",
				)
			} else {
				assertPathMissing(t, completionMarkerPath)
			}

			committed, markerErr := hasPublicationCompletionMarker(
				directory,
				stem,
			)
			if markerErr != nil {
				t.Fatal(markerErr)
			}
			if committed {
				t.Fatal("failed marker commit left the outputs feed-eligible")
			}
			feed := generatePodcastFeedForTest(t, directory)
			if strings.Contains(feed, filepath.Base(audioPath)) ||
				strings.Contains(feed, "uncommitted description") {
				t.Fatalf("feed contains uncommitted publication: %s", feed)
			}
			assertNoDescriptionPublicationStages(t, directory)
		})
	}
}

func TestFailedMarkerLinkFallbackRemainsFeedIneligible(t *testing.T) {
	directory := t.TempDir()
	audioStage := filepath.Join(directory, ".gopod-audio-test.part")
	if err := os.WriteFile(audioStage, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	recordedAt := time.Unix(580, 901)
	token := bytes.Repeat([]byte{0x74}, publicationTokenSize)
	stem := publicationStem("marker-link-failure", recordedAt, token)
	paths := newPublicationPaths(directory, stem)
	injectedErr := errors.New("injected source unlink failure")
	renamer := func(oldPath, newPath string) error {
		if newPath == paths.completionMarker {
			if oldPath != paths.completionStage {
				return fmt.Errorf(
					"marker source = %q, want deterministic stage %q",
					oldPath,
					paths.completionStage,
				)
			}
			if err := os.Link(oldPath, newPath); err != nil {
				return err
			}
			return injectedErr
		}
		return renameNoReplace(oldPath, newPath)
	}

	_, _, err := publishCompletedRecordingWith(
		Podcast{ShortTitle: "marker-link-failure", Directory: directory},
		audioStage,
		"",
		recordedAt,
		bytes.NewReader(token),
		renamer,
	)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("got error %v, want failed marker fallback", err)
	}
	assertFileContent(t, paths.audio, "audio")
	if _, err := os.Lstat(paths.ownership); err != nil {
		t.Fatalf("publication ownership is missing: %v", err)
	}
	stageIdentity, err := os.Lstat(paths.completionStage)
	if err != nil {
		t.Fatal(err)
	}
	markerIdentity, err := os.Lstat(paths.completionMarker)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(stageIdentity, markerIdentity) {
		t.Fatal("failed hard-link fallback did not leave its marker source linked")
	}

	committed, err := hasPublicationCompletionMarker(directory, stem)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("remaining marker source made failed fallback feed-eligible")
	}
	feed := generatePodcastFeedForTest(t, directory)
	if strings.Contains(feed, filepath.Base(paths.audio)) {
		t.Fatalf("feed contains failed marker fallback: %s", feed)
	}
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

func TestPublishFailureLeavesAbandonedDescription(t *testing.T) {
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
	assertFileContent(t, descriptionPath, "description")
	assertFileContent(t, audioStage, "audio")
	assertFileContent(t, descriptionStage, "description")
	assertFileContent(t, existingAudio, "existing audio")
	assertFileContent(t, existingDescription, "existing description")
	assertNoDescriptionPublicationStages(t, directory)
}

func TestPublishFailureNeverTouchesReplacedDescription(t *testing.T) {
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
	audioPath := filepath.Join(directory, stem+".m4a")
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
	assertPathMissing(t, audioPath)
	assertFileContent(t, descriptionPath, "replacement")
	assertFileContent(t, displacedDescription, "description")
	assertFileContent(t, audioStage, "audio")
	assertFileContent(t, descriptionStage, "description")
	assertNoDescriptionPublicationStages(t, directory)
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

func assertNoDescriptionPublicationStages(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(directory, ".gopod-description-publish-*.part"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("disposable description staging files remain: %v", matches)
	}
}

func assertNoOwnedFileCleanupDirectories(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(directory, ownedFileCleanupDirectoryPattern),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("private owned-file cleanup directories remain: %v", matches)
	}
}

func assertCompletionMarker(t *testing.T, directory, stem string) {
	t.Helper()
	committed, err := hasPublicationCompletionMarker(directory, stem)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatalf(
			"completion marker %q is missing or invalid",
			publicationCompletionMarkerPath(directory, stem),
		)
	}
	ownershipID, valid, err := readPublicationIDFile(
		publicationOwnershipPath(directory, stem),
		"ownership",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("persistent publication ownership is missing or invalid")
	}
	completionID, valid, err := readPublicationIDFile(
		publicationCompletionMarkerPath(directory, stem),
		"completion marker",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !valid || completionID != ownershipID {
		t.Fatal("completion marker does not match persistent ownership")
	}
	visibilityID, valid, err := readPublicationIDFile(
		publicationVisibilityMarkerPath(directory, stem),
		"visibility marker",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !valid || visibilityID != ownershipID {
		t.Fatal("visibility marker does not match persistent ownership")
	}
	assertPathMissing(t, publicationCompletionStagePath(directory, stem))
	assertPathMissing(t, publicationVisibilityGuardPath(directory, stem))
}

func assertNoPublicationOwnerships(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(directory, publicationOwnershipPrefix+"*"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("publication ownership files remain: %v", matches)
	}
}

func assertPublicationOwnershipCount(
	t *testing.T,
	directory string,
	want int,
) {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(directory, publicationOwnershipPrefix+"*"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != want {
		t.Fatalf(
			"got %d publication ownership files, want %d: %v",
			len(matches),
			want,
			matches,
		)
	}
}

func generatePodcastFeedForTest(t *testing.T, directory string) string {
	t.Helper()
	if err := GeneratePodcastFeed(Podcast{
		Title:      "Publication test",
		Directory:  directory,
		URL:        "https://example.com/audio/",
		PodcastURL: "https://example.com/podcast",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "podcast.rss"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
