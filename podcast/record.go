package podcast

import (
  "fmt"
  "os"
  "os/exec"
  "time"
)

func Record(podcast Podcast) (string, error) {
  fmt.Println("Recording podcast: " + podcast.Title)
  fmt.Println("Directory: " + podcast.Directory)
  fmt.Println("Image: " + podcast.Image)
  fmt.Println("URL: " + podcast.URL)
  fmt.Println("Podcast URL: " + podcast.PodcastURL)
  fmt.Println("Source URL: " + podcast.SourceURL)
  fmt.Println("Length: ", podcast.Length)
  f, err := os.CreateTemp("", "gopod-recording")
  if err != nil {
    return "", err
  }

  tmpFile := f.Name() + "." + podcast.Extension
  os.Remove(f.Name())
  var t time.Time
  t = t.Add(time.Duration(podcast.Length) * time.Second)
  tFormat := t.Format("15:04:05")
  cmd := exec.Command("ffmpeg", "-i", podcast.SourceURL, "-t", tFormat, "-c", "copy", "-map_metadata", "0", tmpFile)
  err = cmd.Run()
  if err != nil {
    println("Error recording podcast: " + err.Error())
    return "", err
  }
  return tmpFile, nil
}

func Combine(podcast Podcast, audioFile string, metaFile string) (string, error) {
  name := fmt.Sprintf("%s-%d.%s", podcast.ShortTitle, time.Now().Unix(), podcast.Extension)
  fileName := podcast.Directory + "/" + name
  cmd := exec.Command("ffmpeg", "-i", audioFile, "-i", metaFile, "-map_metadata", "1", "-codec", "copy",  fileName)
  fmt.Println("Running command: " + cmd.String())
  err := cmd.Run()
  if err != nil {
    return "", err
  }
  return fileName, nil
}
