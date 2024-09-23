package main

import (
	"encoding/json"
  "fmt"
	"log"
	"os"
	"os/user"
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
	p := loadConfig()
  args := os.Args
  if len(args) > 1 {
    pod, err := podcast.FindPodcast(args[1], p)
    if err != nil {
      log.Fatalf("Could not find podcast %s", args[1])
    }
    podcast.Record(pod)
    podcast.GeneratePodcastFeed(pod.Directory, p)
  } else {
    log.Fatal("No podcast specified")
  }
  fmt.Println("Podcast feed generated")
}
