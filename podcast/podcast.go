package podcast

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eduncan911/podcast"
)

// Podcasts : Array of podcasts
type Podcasts struct {
	Podcasts []*Podcast
}

// Podcast : Details of podcast
type Podcast struct {
	Title      string
	ShortTitle string
	Directory  string
	Image      string
	URL        string
	PodcastURL string
	SourceURL  string
	Extension  string
	Length     int
}

const reservedWorkFilePrefix = ".gopod-"

func ValidateShortTitle(shortTitle string) error {
	switch {
	case shortTitle == "":
		return errors.New("short title must not be empty")
	case shortTitle == "." || shortTitle == "..":
		return fmt.Errorf("short title %q is not allowed", shortTitle)
	case filepath.Base(shortTitle) != shortTitle:
		return fmt.Errorf("short title %q must not contain path separators", shortTitle)
	case strings.HasPrefix(shortTitle+"-", reservedWorkFilePrefix):
		return fmt.Errorf(
			"short title %q would create output filenames with reserved prefix %q",
			shortTitle,
			reservedWorkFilePrefix,
		)
	default:
		return nil
	}
}

// Event : Filesystem change data
type Event struct {
	Path  string
	Event string
}

type ByModTime []os.FileInfo

func (files ByModTime) Len() int {
	return len(files)
}

func (files ByModTime) Swap(i, j int) {
	files[i], files[j] = files[j], files[i]
}

func (files ByModTime) Less(i, j int) bool {
	return files[i].ModTime().Before(files[j].ModTime())
}

func FindPodcast(shortName string, p Podcasts) (Podcast, error) {
	podcast := Podcast{}
	found := false
	for _, pc := range p.Podcasts {
		if pc.ShortTitle == shortName {
			podcast = *pc
			found = true
		}
	}
	if !found {
		return podcast, errors.New("Podcast not found")
	}
	return podcast, nil
}

func GeneratePodcastFeed(podcastData Podcast) error {
	fullDir := podcastData.Directory
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return fmt.Errorf("read podcast directory %q: %w", fullDir, err)
	}

	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), reservedWorkFilePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read podcast file info %q: %w", entry.Name(), err)
		}
		files = append(files, info)
	}
	sort.Sort(ByModTime(files))
	pubDate := time.Now()
	updatedDate := time.Now()

	feed := podcast.New(
		podcastData.Title,
		podcastData.PodcastURL,
		podcastData.Title,
		&pubDate, &updatedDate,
	)
	feed.AddImage(podcastData.Image)

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".mp3" || filepath.Ext(file.Name()) == ".aac" || filepath.Ext(file.Name()) == ".m4a" {
			podcastTime := file.ModTime()
			title := podcastData.Title + " " + file.ModTime().Format(time.RFC850)
			var podcastDownload strings.Builder
			podcastDownload.WriteString(podcastData.URL)
			podcastDownload.WriteString(file.Name())

			description := "📻🤖"
			descriptionFile := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())) + ".txt"
			data, err := os.ReadFile(filepath.Join(fullDir, descriptionFile))
			if err == nil {
				description = string(data)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read podcast description %q: %w", descriptionFile, err)
			}

			item := podcast.Item{
				Title:       title,
				Link:        podcastDownload.String(),
				Description: description,
				PubDate:     &podcastTime,
			}
			item.AddImage(podcastData.Image)
			if filepath.Ext(file.Name()) == ".mp3" {
				item.AddEnclosure(podcastDownload.String(), podcast.MP3, file.Size())
			} else {
				item.AddEnclosure(podcastDownload.String(), podcast.M4A, file.Size())
			}
			if _, err := feed.AddItem(item); err != nil {
				return fmt.Errorf("add podcast item %q: %w", file.Name(), err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(fullDir, "podcast.rss"), feed.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write podcast feed: %w", err)
	}
	return nil
}
