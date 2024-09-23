package podcast

import (
  "fmt"
)

func Record(podcast Podcast) {
  fmt.Println("Recording podcast: " + podcast.Title)
  fmt.Println("Directory: " + podcast.Directory)
  fmt.Println("Image: " + podcast.Image)
  fmt.Println("URL: " + podcast.URL)
  fmt.Println("Podcast URL: " + podcast.PodcastURL)
  fmt.Println("Source URL: " + podcast.SourceURL)
  fmt.Println("Length: ", podcast.Length)
  streamTitle, err := GetStreamTitle(podcast.SourceURL)
  if err != nil {
    fmt.Println("Error getting stream title: " + err.Error())
  }
  fmt.Println("Current Title: " + streamTitle)
}
