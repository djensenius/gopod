package podcast

import (
  "fmt"
  "os"
  "os/exec"
  "time"
)

func Record(podcast Podcast) (string, error) {
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

func Combine(podcast Podcast, audioFile string, metaFile string, descriptionFile string) (error) {
  nowTime := time.Now().Unix()
  name := fmt.Sprintf("%s-%d.m4a", podcast.ShortTitle, nowTime)
  fileName := podcast.Directory + "/" + name
  cmd := exec.Command("ffmpeg", "-i", audioFile, "-i", metaFile, "-map_metadata", "1", "-c:a", "aac",  fileName)
  err := cmd.Run()
  if err != nil {
    return err
  }

  descName := fmt.Sprintf("%s-%d.txt", podcast.ShortTitle, nowTime)
  descFileName := podcast.Directory + "/" + descName
  if descriptionFile != "" {
    data, err := os.ReadFile(descriptionFile)
    if err != nil {
      return err
    }
    writeErr := os.WriteFile(descFileName, data, 0644)
    if writeErr != nil {
      return writeErr
    }
  }

  return nil
}
