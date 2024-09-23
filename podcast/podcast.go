package podcast

import (
	"fmt"
	"os"
  "errors"
	"path/filepath"
	"sort"
	"time"
	"strings"
	"github.com/eduncan911/podcast"
)

// Podcasts : Array of podcasts
type Podcasts struct {
	Podcasts []*Podcast
}

// Podcast : Details of podcast
type Podcast struct {
	Title     string
  ShortTitle string
	Directory string
	Image     string
	URL       string
	PodcastURL string
  SourceURL string
  Length    int
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

func getPodcastData(path string, p Podcasts) Podcast {
	var podcastData Podcast
	dir := filepath.Dir(path + "/")
	for _, pc := range p.Podcasts {
		fp, err := filepath.EvalSymlinks(pc.Directory)
		if (err != nil) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if fp == dir {
			podcastData = Podcast {
				Directory: pc.Directory,
				Title: pc.Title,
				Image: pc.Image,
				URL: pc.URL,
				PodcastURL: pc.PodcastURL,
			}
			return podcastData
		}
	}
	return podcastData
}

func GeneratePodcastFeed(path string, p Podcasts) {
	podcastData := getPodcastData(path, p)
	fullDir := filepath.Dir(path + "/")
	f, _ := os.Open(fullDir)
	files, _ := f.Readdir(-1)
	f.Close()
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
		if filepath.Ext(file.Name()) == ".mp3" || filepath.Ext(file.Name()) == ".aac" {
			podcastTime := file.ModTime()
			title := podcastData.Title + " " + file.ModTime().Format(time.RFC850)
			var podcastDownload strings.Builder
			podcastDownload.WriteString(podcastData.URL)
			podcastDownload.WriteString(file.Name())
			item := podcast.Item{
				Title:       title,
				Link:        podcastDownload.String(),
				Description: "📻🤖",
				PubDate:     &podcastTime,
			}
			item.AddImage(podcastData.Image)
			if filepath.Ext(file.Name()) == ".mp3" {
				item.AddEnclosure(podcastDownload.String(), podcast.MP3, file.Size())
			} else {
				item.AddEnclosure(podcastDownload.String(), podcast.M4A, file.Size())
			}
			if _, err := feed.AddItem(item); err != nil {
				os.Stderr.WriteString("item validation error: " + err.Error())
			}
		}
	}
	if err := os.WriteFile(fullDir+"/podcast.rss", feed.Bytes(), 0755); err != nil {
		fmt.Println("error writing to stdout:", err.Error())
	}
}
