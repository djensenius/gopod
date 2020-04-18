package main

import (
  "encoding/json"
  "os"
  "fmt"
	// "log"
	// "github.com/fsnotify/fsnotify"
)

type Podcasts struct {
  Podcasts []*Podcast
}

type Podcast struct {
  Title string
  Directory string
  Image string
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

func main() {
  p := loadConfig()
  fmt.Println(p.Podcasts[0].Title)
}
