package podcast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestGeneratePodcastFeedIgnoresReservedWorkFiles(t *testing.T) {
	directory := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(directory, "episode.m4a"):                 "completed audio",
		filepath.Join(directory, "episode.txt"):                 "completed description",
		filepath.Join(directory, ".gopod-audio-stale.m4a"):      "stale audio",
		filepath.Join(directory, ".gopod-recording-stale.mp3"):  "stale recording",
		filepath.Join(directory, ".gopod-audio-current.part"):   "current audio",
		filepath.Join(directory, ".gopod-description-old.part"): "old description",
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
	} {
		if strings.Contains(feed, reservedName) {
			t.Fatalf("feed contains reserved work file %q", reservedName)
		}
	}
}
