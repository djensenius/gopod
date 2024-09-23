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
}
