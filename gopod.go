package main

import (
	"encoding/json"
  "fmt"
	"log"
	"os"
	"os/user"
  "sync"
  "time"
  "gopod/podcast"
)

func loadConfig() podcast.Podcasts {
	user, err := user.Current()
	if err != nil {
    log.Fatalf("Could not find user: %s", err.Error())
	}
	homeDirectory := user.HomeDir
	config, err := os.Open(homeDirectory + "/.config/gopod/config.json")
  errorPresent := false
	if err != nil {
    log.Printf("Could not find config file: %s, using default config", err.Error())
    errorPresent = true
	}

  if errorPresent {
    config, _ = os.Open("config.json.sample")
  }

	defer config.Close()
	decoder := json.NewDecoder(config)
	podcasts := podcast.Podcasts{}
	err = decoder.Decode(&podcasts)
	if err != nil {
		log.Fatalf("Could not decode podcast %s", err.Error())
	}
	return podcasts
}

func main() {
  var Reset = "\033[0m" 
  var Green = "\033[32m"
  var wg sync.WaitGroup
	p := loadConfig()
  args := os.Args
  if len(args) > 1 {
    pod, err := podcast.FindPodcast(args[1], p)
    if err != nil {
      log.Fatalf("Could not find podcast %s", args[1])
    }
    meta := make(chan string)
    record := make(chan string)

    wg.Add(1)
    go func(meta chan string) {
      defer wg.Done()
      metaFile, descriptionFile, err := podcast.MonitorStream(pod.SourceURL, time.Duration(pod.Length) * time.Second)
      if err != nil {
        log.Fatalf("Could not monitor stream %s", err.Error())
      }
      meta <-metaFile
      meta <-descriptionFile
    }(meta)

    wg.Add(1)
    go func(record chan string) {
      defer wg.Done()
      recordedFile, err := podcast.Record(pod)
      if err != nil {
        log.Fatalf("Could not record podcast %s", err.Error())
      }
      record <-recordedFile
    }(record)

    recordedFile := <-record
    metaFile := <-meta
    descriptionFile := <-meta
    wg.Wait()

    combineErr := podcast.Combine(pod, recordedFile, metaFile, descriptionFile)
    if combineErr != nil {
      log.Fatalf("Could not combine podcast %s", combineErr.Error())
    }
    os.Remove(recordedFile)
    os.Remove(metaFile)
    os.Remove(descriptionFile)

    podcast.GeneratePodcastFeed(pod.Directory, p)
  } else {
    log.Fatal("No podcast specified")
  }
  fmt.Println(Green + "✅ Podcast feed generated" + Reset)
}
