package podcast

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateShortTitle(t *testing.T) {
	for _, shortTitle := range []string{
		"show",
		"show.v2",
		".show",
		".gopod.show",
	} {
		t.Run("accepts "+shortTitle, func(t *testing.T) {
			if err := ValidateShortTitle(shortTitle); err != nil {
				t.Fatalf("ValidateShortTitle(%q) returned %v", shortTitle, err)
			}
		})
	}

	tests := []struct {
		name       string
		shortTitle string
		want       string
	}{
		{name: "empty", shortTitle: "", want: "must not be empty"},
		{name: "dot", shortTitle: ".", want: "is not allowed"},
		{name: "dot dot", shortTitle: "..", want: "is not allowed"},
		{
			name:       "path separator",
			shortTitle: filepath.Join("nested", "show"),
			want:       "path separators",
		},
		{name: "reserved base", shortTitle: ".gopod", want: `reserved prefix ".gopod-"`},
		{name: "reserved prefix", shortTitle: ".gopod-show", want: `reserved prefix ".gopod-"`},
	}
	for _, test := range tests {
		t.Run("rejects "+test.name, func(t *testing.T) {
			err := ValidateShortTitle(test.shortTitle)
			if err == nil {
				t.Fatalf("ValidateShortTitle(%q) unexpectedly succeeded", test.shortTitle)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not contain %q", err, test.want)
			}
		})
	}
}

func TestFindPodcastSkipsNilEntries(t *testing.T) {
	configured := &Podcast{
		Title:      "Configured Show",
		ShortTitle: "configured",
	}
	podcasts := Podcasts{
		Podcasts: []*Podcast{nil, configured},
	}

	got, err := FindPodcast(configured.ShortTitle, podcasts)
	if err != nil {
		t.Fatal(err)
	}
	if got != *configured {
		t.Fatalf("got podcast %#v, want %#v", got, *configured)
	}

	if _, err := FindPodcast("missing", Podcasts{
		Podcasts: []*Podcast{nil},
	}); err == nil {
		t.Fatal("expected missing podcast error")
	}
}

func TestParseGeneratedPublicationStemFromEnd(t *testing.T) {
	recordedAt := time.Unix(1_700_000_000, 123)
	token := bytes.Repeat([]byte{0xab}, publicationTokenSize)
	for _, shortTitle := range []string{
		"show-with-hyphens",
		"show.with.dots",
		"show.with-dots-and-hyphens",
		"show-gopod1-with-protocol-text",
		"show-",
	} {
		t.Run(shortTitle, func(t *testing.T) {
			stem := publicationStem(shortTitle, recordedAt, token)
			wantStem := shortTitle +
				"-1700000000000000123-" +
				publicationProtocol +
				"-" +
				strings.Repeat("ab", publicationTokenSize)
			if stem != wantStem {
				t.Fatalf("got publication stem %q, want %q", stem, wantStem)
			}
			gotShortTitle, gotUnixNano, ok := parseGeneratedPublicationStem(stem)
			if !ok {
				t.Fatalf("parseGeneratedPublicationStem(%q) failed", stem)
			}
			if gotShortTitle != shortTitle {
				t.Fatalf(
					"got short title %q, want %q",
					gotShortTitle,
					shortTitle,
				)
			}
			if gotUnixNano != recordedAt.UnixNano() {
				t.Fatalf(
					"got UnixNano %d, want %d",
					gotUnixNano,
					recordedAt.UnixNano(),
				)
			}
		})
	}
}

func TestParseGeneratedPublicationStemRejectsLegacyAndMalformedNames(t *testing.T) {
	lowerToken := strings.Repeat("a", publicationTokenSize*2)
	for _, stem := range []string{
		"legacy-1700000000",
		"head-format-1700000000123000000-0123456789abcdef0123456789abcdef",
		"missing-timestamp-" + publicationProtocol + "-" + lowerToken,
		"wrong-protocol-1700000000123000000-gopod2-" + lowerToken,
		"leading-zero-01700000000123000000-" + publicationProtocol + "-" +
			lowerToken,
		"short-token-1700000000123000000-" + publicationProtocol + "-" +
			lowerToken[:len(lowerToken)-1],
		"bad-token-1700000000123000000-" + publicationProtocol + "-" +
			strings.Repeat("g", len(lowerToken)),
		"uppercase-token-1700000000123000000-" + publicationProtocol + "-" +
			strings.ToUpper(lowerToken),
	} {
		t.Run(stem, func(t *testing.T) {
			if _, _, ok := parseGeneratedPublicationStem(stem); ok {
				t.Fatalf(
					"parseGeneratedPublicationStem(%q) treated a legacy or malformed name as generated",
					stem,
				)
			}
		})
	}
}

func TestGeneratePodcastFeedIncludesPriorHeadFormatWithoutMarkers(t *testing.T) {
	directory := t.TempDir()
	const audioName = "head-format-1700000000123000000-" +
		"0123456789abcdef0123456789abcdef.m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("prior HEAD audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	feed := generatePodcastFeedForTest(t, directory)
	if !strings.Contains(feed, audioName) {
		t.Fatalf("feed does not contain prior HEAD filename %q: %s", audioName, feed)
	}
}

func TestGeneratePodcastFeedIncludesMarkedGeneratedEpisode(t *testing.T) {
	directory := t.TempDir()
	stem := publicationStem(
		"feed.with-dots-and-hyphens",
		time.Unix(1_700_000_000, 123),
		bytes.Repeat([]byte{0x41}, publicationTokenSize),
	)
	audioName := stem + ".m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("completed generated audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(directory, stem+".txt"),
		[]byte("completed generated description"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writePublicationControlPair(
		t,
		directory,
		stem,
		strings.Repeat("1", publicationIDTextLength),
	)

	feed := generatePodcastFeedForTest(t, directory)
	if !strings.Contains(feed, audioName) ||
		!strings.Contains(feed, "completed generated description") {
		t.Fatalf("feed does not contain the committed generated episode: %s", feed)
	}
}

func TestGeneratePodcastFeedExcludesUnmarkedGeneratedEpisode(t *testing.T) {
	directory := t.TempDir()
	stem := publicationStem(
		"uncommitted-feed",
		time.Unix(1_700_000_001, 456),
		bytes.Repeat([]byte{0x42}, publicationTokenSize),
	)
	audioName := stem + ".m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("uncommitted generated audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(directory, stem+".txt"),
		[]byte("uncommitted generated description"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	feed := generatePodcastFeedForTest(t, directory)
	if strings.Contains(feed, audioName) ||
		strings.Contains(feed, "uncommitted generated description") {
		t.Fatalf("feed contains an unmarked generated episode: %s", feed)
	}
}

func TestGeneratePodcastFeedExcludesCompletionWithCommitStage(
	t *testing.T,
) {
	directory := t.TempDir()
	stem := publicationStem(
		"interrupted-commit",
		time.Unix(1_700_000_002, 789),
		bytes.Repeat([]byte{0x43}, publicationTokenSize),
	)
	audioName := stem + ".m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	publicationID := strings.Repeat("2", publicationIDTextLength)
	writePublicationControlPair(t, directory, stem, publicationID)
	if err := os.WriteFile(
		publicationCompletionStagePath(directory, stem),
		[]byte(publicationID),
		publicationControlFileMode,
	); err != nil {
		t.Fatal(err)
	}

	feed := generatePodcastFeedForTest(t, directory)
	if strings.Contains(feed, audioName) {
		t.Fatalf(
			"feed contains an episode whose commit stage still exists: %s",
			feed,
		)
	}
}

func TestGeneratePodcastFeedExcludesCompletionWithVisibilityGuard(
	t *testing.T,
) {
	directory := t.TempDir()
	stem := publicationStem(
		"guarded-commit",
		time.Unix(1_700_000_002, 790),
		bytes.Repeat([]byte{0x44}, publicationTokenSize),
	)
	audioName := stem + ".m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	publicationID := strings.Repeat("8", publicationIDTextLength)
	writePublicationControlPair(t, directory, stem, publicationID)
	if err := os.WriteFile(
		publicationVisibilityGuardPath(directory, stem),
		[]byte(publicationID),
		publicationControlFileMode,
	); err != nil {
		t.Fatal(err)
	}

	feed := generatePodcastFeedForTest(t, directory)
	if strings.Contains(feed, audioName) {
		t.Fatalf(
			"feed contains an episode whose visibility guard still exists: %s",
			feed,
		)
	}
}

func TestGeneratePodcastFeedRequiresMatchingValidPublicationIDs(t *testing.T) {
	tests := []struct {
		name        string
		ownershipID string
		completion  string
	}{
		{
			name:        "mismatched",
			ownershipID: strings.Repeat("3", publicationIDTextLength),
			completion:  strings.Repeat("4", publicationIDTextLength),
		},
		{
			name:        "empty completion",
			ownershipID: strings.Repeat("5", publicationIDTextLength),
			completion:  "",
		},
		{
			name:        "uppercase completion",
			ownershipID: strings.Repeat("6", publicationIDTextLength),
			completion:  strings.Repeat("A", publicationIDTextLength),
		},
		{
			name:        "empty ownership",
			ownershipID: "",
			completion:  strings.Repeat("7", publicationIDTextLength),
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			stem := publicationStem(
				"invalid-marker",
				time.Unix(1_700_000_003+int64(index), 123),
				bytes.Repeat(
					[]byte{byte(0x50 + index)},
					publicationTokenSize,
				),
			)
			audioName := stem + ".m4a"
			if err := os.WriteFile(
				filepath.Join(directory, audioName),
				[]byte("audio"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				publicationOwnershipPath(directory, stem),
				[]byte(test.ownershipID),
				publicationControlFileMode,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				publicationCompletionMarkerPath(directory, stem),
				[]byte(test.completion),
				publicationControlFileMode,
			); err != nil {
				t.Fatal(err)
			}

			feed := generatePodcastFeedForTest(t, directory)
			if strings.Contains(feed, audioName) {
				t.Fatalf(
					"feed contains an episode with %s publication IDs: %s",
					test.name,
					feed,
				)
			}
		})
	}
}

func TestGeneratePodcastFeedIncludesLegacyAndMalformedNamesWithoutMarkers(
	t *testing.T,
) {
	directory := t.TempDir()
	legacyName := "legacy-show-1700000000.m4a"
	malformedName := "archive-not-a-time-" +
		strings.Repeat("a", publicationTokenSize*2) +
		".m4a"
	for _, name := range []string{legacyName, malformedName} {
		if err := os.WriteFile(
			filepath.Join(directory, name),
			[]byte("legacy audio"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	feed := generatePodcastFeedForTest(t, directory)
	for _, name := range []string{legacyName, malformedName} {
		if !strings.Contains(feed, name) {
			t.Fatalf("feed does not contain legacy filename %q: %s", name, feed)
		}
	}
}

func TestGeneratePodcastFeedIgnoresReservedWorkFiles(t *testing.T) {
	directory := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(directory, "episode.m4a"):                 "completed audio",
		filepath.Join(directory, "episode.txt"):                 "completed description",
		filepath.Join(directory, ".gopod-audio-stale.m4a"):      "stale audio",
		filepath.Join(directory, ".gopod-recording-stale.mp3"):  "stale recording",
		filepath.Join(directory, ".gopod-audio-current.part"):   "current audio",
		filepath.Join(directory, ".gopod-description-old.part"): "old description",
		filepath.Join(directory, ".gopod-owner-stale"):          "stale ownership",
		filepath.Join(directory, ".gopod-complete-stale"):       "stale completion",
		filepath.Join(directory, ".gopod-commit-stale"):         "stale commit stage",
		filepath.Join(directory, ".gopod-guard-stale"):          "stale visibility guard",
		filepath.Join(directory, ".gopod-visible-stale"):        "stale visibility marker",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := GeneratePodcastFeed(Podcast{
		Title:      "Feed test",
		Directory:  directory,
		URL:        "https://example.com/audio/",
		PodcastURL: "https://example.com/podcast.rss",
	})
	if err != nil {
		t.Fatal(err)
	}

	feedData, err := os.ReadFile(filepath.Join(directory, "podcast.rss"))
	if err != nil {
		t.Fatal(err)
	}
	feed := string(feedData)
	if !strings.Contains(feed, "episode.m4a") {
		t.Fatalf("feed does not contain completed episode: %s", feed)
	}
	for _, reservedName := range []string{
		".gopod-audio-stale.m4a",
		".gopod-recording-stale.mp3",
		".gopod-audio-current.part",
		".gopod-description-old.part",
		".gopod-owner-stale",
		".gopod-complete-stale",
		".gopod-commit-stale",
		".gopod-guard-stale",
		".gopod-visible-stale",
	} {
		if strings.Contains(feed, reservedName) {
			t.Fatalf("feed contains reserved work file %q", reservedName)
		}
	}
}

func TestPublicationControlPathsUseNormalizationStableSuffixKey(t *testing.T) {
	directory := t.TempDir()
	recordedAt := time.Unix(1_700_000_000, 123_456_789)
	token := bytes.Repeat([]byte{0xaa}, publicationTokenSize)
	stem := publicationStem(
		strings.Repeat("long-title-", 20)+"Café",
		recordedAt,
		token,
	)
	decomposedStem := publicationStem(
		strings.Repeat("long-title-", 20)+"Cafe\u0301",
		recordedAt,
		token,
	)
	otherTimestampStem := publicationStem(
		"other-title",
		recordedAt.Add(time.Nanosecond),
		token,
	)
	otherTokenStem := publicationStem(
		"other-title",
		recordedAt,
		bytes.Repeat([]byte{0xbb}, publicationTokenSize),
	)

	paths := []string{
		publicationOwnershipPath(directory, stem),
		publicationCompletionMarkerPath(directory, stem),
		publicationCompletionStagePath(directory, stem),
		publicationVisibilityGuardPath(directory, stem),
		publicationVisibilityMarkerPath(directory, stem),
	}
	for _, path := range paths {
		if len(filepath.Base(path)) >= 255 {
			t.Fatalf("internal publication path is too long: %q", path)
		}
		if strings.Contains(filepath.Base(path), stem) {
			t.Fatalf("internal publication path embeds the full stem: %q", path)
		}
	}
	if publicationOwnershipPath(directory, stem) ==
		publicationOwnershipPath(directory, otherTimestampStem) {
		t.Fatal("different ASCII timestamps produced the same ownership path")
	}
	if publicationOwnershipPath(directory, stem) ==
		publicationOwnershipPath(directory, otherTokenStem) {
		t.Fatal("different ASCII tokens produced the same ownership path")
	}
	if publicationOwnershipPath(directory, stem) !=
		publicationOwnershipPath(directory, decomposedStem) {
		t.Fatal("Unicode-normalization-equivalent titles produced different keys")
	}
	completionMarkerPath := publicationCompletionMarkerPath(directory, stem)
	if completionMarkerPath !=
		publicationCompletionMarkerPath(directory, stem) {
		t.Fatal("completion marker path is not deterministic")
	}
}

func TestGeneratePodcastFeedUsesControlsAcrossUnicodeNormalization(t *testing.T) {
	directory := t.TempDir()
	recordedAt := time.Unix(1_700_000_005, 987_654_321)
	token := bytes.Repeat([]byte{0xcc}, publicationTokenSize)
	nfcStem := publicationStem("Café", recordedAt, token)
	nfdStem := publicationStem("Cafe\u0301", recordedAt, token)
	audioName := nfdStem + ".m4a"
	if err := os.WriteFile(
		filepath.Join(directory, audioName),
		[]byte("normalized audio"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writePublicationControlPair(
		t,
		directory,
		nfcStem,
		strings.Repeat("9", publicationIDTextLength),
	)

	eligible, err := hasPublicationCompletionMarker(directory, nfdStem)
	if err != nil {
		t.Fatal(err)
	}
	if !eligible {
		t.Fatal("NFD feed lookup did not find controls created from the NFC stem")
	}
	feed := generatePodcastFeedForTest(t, directory)
	if strings.Count(feed, "<item>") != 1 ||
		!strings.Contains(feed, strings.Repeat("cc", publicationTokenSize)) {
		t.Fatalf("feed does not contain the normalization-stable episode: %s", feed)
	}
}

func writePublicationControlPair(
	t *testing.T,
	directory string,
	stem string,
	publicationID string,
) {
	t.Helper()
	for _, path := range []string{
		publicationOwnershipPath(directory, stem),
		publicationCompletionMarkerPath(directory, stem),
		publicationVisibilityMarkerPath(directory, stem),
	} {
		if err := os.WriteFile(
			path,
			[]byte(publicationID),
			publicationControlFileMode,
		); err != nil {
			t.Fatal(err)
		}
	}
}
