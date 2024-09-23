// Taken and modified from https://gist.github.com/jucrouzet/3e59877c0b4352966e6220034f2b84ac
package podcast

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
  "os"
	"strconv"
  "time"
)

// GetStreamTitle get the current song/show in an Icecast stream
func GetStreamTitle(streamUrl string) (string, error) {
	m, err := getStreamMetas(streamUrl)

	if err != nil {
		return "", err
	}
	// Should be at least "StreamTitle=' '"
	if len(m) < 15 {
		return "", nil
	}
	// Split meta by ';', trim it and search for StreamTitle
	for _, m := range bytes.Split(m, []byte(";")) {
		m = bytes.Trim(m, " \t")
		if !bytes.Equal(m[0:13], []byte("StreamTitle='")) {
			continue
		}
		return string(m[13 : len(m)-1]), nil
	}
	return "", nil
}

// get stream metadatas
func getStreamMetas(streamUrl string) ([]byte, error) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", streamUrl, nil)
	req.Header.Set("Icy-MetaData", "1")
	resp, err := client.Do(req)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	// We sent "Icy-MetaData", we should have a "icy-metaint" in return
	ih := resp.Header.Get("icy-metaint")
	if ih == "" {
		resp.Body.Close()
		return nil, fmt.Errorf("no metadata")
	}
	// "icy-metaint" is how often (in bytes) should we receive the meta
	ib, err := strconv.Atoi(ih)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	reader := bufio.NewReader(resp.Body)

	// skip the first mp3 frame
	c, err := reader.Discard(ib)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	// If we didn't received ib bytes, the stream is ended
	if c != ib {
		resp.Body.Close()
		return nil, fmt.Errorf("stream ended prematurally")
	}

	// get the size byte, that is the metadata length in bytes / 16
	sb, err := reader.ReadByte()
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	ms := int(sb * 16)

	// read the ms first bytes it will contain metadata
	m, err := reader.Peek(ms)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	resp.Body.Close()
	return m, nil
}

func MonitorStream(streamUrl string, duration time.Duration) (string, error) {
  overallProgress := time.Now()
  fileContent := ";FFMETADATA1\n\n"
  formerTitle := ""
  count := 0

  for time.Since(overallProgress) < duration {
    start := time.Now()
    title, err := GetStreamTitle(streamUrl)
    if err != nil {
      return "", err
    }

    if title != formerTitle {
      if formerTitle != "" {
        fileContent += "END=" + strconv.Itoa(count) + "\n"
        fileContent += "title=" + formerTitle + "\n\n"
      }

      fileContent += "[CHAPTER]\n"
      fileContent += "TIMEBASE=1/1\n"
      fileContent += "START=" + strconv.Itoa(count + 1) + "\n"
      if title != "" {
        formerTitle = title
      } else {
        formerTitle = "Unknown"
      }
    }

    // Don't query more than once a second
    timeLeft := time.Second - time.Since(start)
    if timeLeft > 0 {
      time.Sleep(timeLeft)
    }
    count += 1
  }

  fileContent += "END=" + strconv.Itoa(count) + "\n"
  fileContent += "title=" + formerTitle + "\n\n"
  f, err := os.CreateTemp("", "*.txt")
  if err != nil {
    return "", err
  }

  fmt.Println("Writing file: " + f.Name())
  fmt.Println("File content: " + fileContent)
  if _, err := f.Write([]byte(fileContent)); err != nil {
    return "", err
  }

  return f.Name(), nil
}
