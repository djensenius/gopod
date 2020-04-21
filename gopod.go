package main

import (
  "encoding/json"
  "os"
  "fmt"
  "sort"
  "time"
  "io/ioutil"
  "path/filepath"
  "github.com/rjeczalik/notify"
  "github.com/eduncan911/podcast"
)

type Podcasts struct {
  Podcasts []*Podcast
}

type Podcast struct {
  Title string
  Directory string
  Image string
  URL string
}

func loadConfig() Podcasts {
  config, _ := os.Open("config.json")
  defer config.Close()
  decoder := json.NewDecoder(config)
  podcasts := Podcasts{}
  err := decoder.Decode(&podcasts)
  if err != nil {
    fmt.Println("error:", err)
  }
  return podcasts
}

var mapping = map[notify.Event]string{
	notify.Create: "create",
	notify.Remove: "remove",
	notify.Rename: "rename",
	notify.Write:  "write",
}

type Event struct {
	Path  string
	Event string
}

func newEvent(ei notify.EventInfo) Event {
	return Event{
		Path:  ei.Path(),
		Event: mapping[ei.Event()],
	}
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

func generatePodcastFeed(path string, p Podcasts) {
  var podcastData *Podcast
  dir := filepath.Dir(path)
  for _, pc := range p.Podcasts {
    if pc.Directory == dir {
      podcastData = pc
    }
  }

  f, _ := os.Open(dir)
  files, _ := f.Readdir(-1)
  f.Close()
  sort.Sort(ByModTime(files))
  pubDate := time.Now()
  updatedDate := time.Now()

  feed := podcast.New(
    podcastData.Title,
    "",
    podcastData.Title,
    &pubDate, &updatedDate,
  )
  feed.AddImage(podcastData.Image)

  for _, file := range files {
    if filepath.Ext(file.Name()) == ".mp3" {
      podcastTime := file.ModTime()
      item := podcast.Item {
        Title: file.ModTime().Format("ANSIC"),
        Description: "📻🤖",
        PubDate: &podcastTime,
      }
      item.AddImage(podcastData.Image)
      item.AddEnclosure(podcastData.URL + file.Name(), podcast.MP3, file.Size())
      if _, err := feed.AddItem(item); err != nil {
        os.Stderr.WriteString("item validation error: " + err.Error())
      }
    }
  }
  if err := ioutil.WriteFile(dir + "/podcast.rss", feed.Bytes(), 0755); err != nil {
    fmt.Println("error writing to stdout:", err.Error())
  }
}

func watchDirectories(p Podcasts) {
  c := make(chan notify.EventInfo, 1)
	for _, path := range p.Podcasts {
		if err := notify.Watch(path.Directory, c, notify.All); err != nil {
	    fmt.Fprintln(os.Stderr, err)
	    os.Exit(1)
		}
	}
	for ei := range c {
		e := newEvent(ei)
    if e.Event == "create" && filepath.Ext(e.Path) == ".mp3" {
      generatePodcastFeed(e.Path, p)
    }
	}
}

func main() {
  p := loadConfig()
  watchDirectories(p)
}
