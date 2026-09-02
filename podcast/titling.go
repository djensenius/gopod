// Taken and modified from https://gist.github.com/jucrouzet/3e59877c0b4352966e6220034f2b84ac
package podcast

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

const streamMetadataTimeout = 30 * time.Second

// GetStreamTitle get the current song/show in an Icecast stream
func GetStreamTitle(ctx context.Context, streamURL string) (string, error) {
	m, err := getStreamMetas(ctx, streamURL)

	if err != nil {
		return "", err
	}
	// Should be at least "StreamTitle=' '"
	if len(m) < 15 {
		return "", nil
	}
	// Split meta by ';', trim it and search for StreamTitle
	for _, metadata := range bytes.Split(m, []byte(";")) {
		metadata = bytes.Trim(metadata, " \t\x00")
		const prefix = "StreamTitle='"
		if !bytes.HasPrefix(metadata, []byte(prefix)) {
			continue
		}
		value := bytes.TrimSuffix(metadata[len(prefix):], []byte("'"))
		return string(value), nil
	}
	return "", nil
}

// get stream metadatas
func getStreamMetas(ctx context.Context, streamURL string) ([]byte, error) {
	client := &http.Client{Timeout: streamMetadataTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Icy-MetaData", "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("metadata request returned %s", resp.Status)
	}

	// We sent "Icy-MetaData", we should have a "icy-metaint" in return
	ih := resp.Header.Get("icy-metaint")
	if ih == "" {
		return nil, fmt.Errorf("no metadata")
	}
	// "icy-metaint" is how often (in bytes) should we receive the meta
	ib, err := strconv.Atoi(ih)
	if err != nil {
		return nil, err
	}
	if ib <= 0 {
		return nil, fmt.Errorf("invalid icy-metaint %q", ih)
	}

	reader := bufio.NewReader(resp.Body)

	// skip the first mp3 frame
	c, err := reader.Discard(ib)
	if err != nil {
		return nil, err
	}

	// If we didn't received ib bytes, the stream is ended
	if c != ib {
		return nil, fmt.Errorf("stream ended prematurally")
	}

	// get the size byte, that is the metadata length in bytes / 16
	sb, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	ms := int(sb * 16)

	// read the ms first bytes it will contain metadata
	m := make([]byte, ms)
	if _, err := io.ReadFull(reader, m); err != nil {
		return nil, err
	}

	return m, nil
}

func MonitorStream(
	ctx context.Context,
	streamURL string,
	duration time.Duration,
	title string,
) (string, string, error) {
	overallProgress := time.Now()
	fileContent := ";FFMETADATA1\n\n"
	formerTitle := ""
	count := 0
	chapterStart := 0
	notes := ""

	bar := progressbar.NewOptions(int(duration.Seconds()),
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(false),
		progressbar.OptionSetWidth(25),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetDescription("[green]🔴 Recording podcast: [blue]"+title+"[reset]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	for time.Since(overallProgress) < duration {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}

		start := time.Now()
		streamTitle, err := GetStreamTitle(ctx, streamURL)
		if err != nil {
			return "", "", err
		}

		if streamTitle != formerTitle {
			if formerTitle != "" {
				fileContent += "END=" + strconv.Itoa(count) + "\n"
				fileContent += "title=" + formerTitle + "\n\n"
				params := url.Values{}
				params.Add("term", formerTitle)

				bandCampParams := url.Values{}
				bandCampParams.Add("q", formerTitle)
				startFormat, endFormat := formatChapterRange(chapterStart, count)

				notes += "[" + startFormat + " - " + endFormat + "]: " + formerTitle + "\n"
				notes += "<a href=\"https://music.apple.com/ca/search?" + params.Encode() + "\">Apple Music</a> | "
				notes += "<a href=\"https://bandcamp.com/search?" + bandCampParams.Encode() + "\">Bandcamp</a><br /> "
			}

			fileContent += "[CHAPTER]\n"
			fileContent += "TIMEBASE=1/1\n"
			fileContent += "START=" + strconv.Itoa(count+1) + "\n"
			chapterStart = count
			if streamTitle != "" {
				formerTitle = streamTitle
			} else {
				formerTitle = "Unknown"
			}
		}

		// Don't query more than once a second
		timeLeft := time.Second - time.Since(start)
		if timeLeft > 0 {
			timer := time.NewTimer(timeLeft)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return "", "", ctx.Err()
			case <-timer.C:
			}
		}
		if err := bar.Add(1); err != nil {
			return "", "", fmt.Errorf("update recording progress: %w", err)
		}
		count += 1
	}

	fileContent += "END=" + strconv.Itoa(count) + "\n"
	fileContent += "title=" + formerTitle + "\n\n"

	startFormat, endFormat := formatChapterRange(chapterStart, count)

	notes += "[" + startFormat + " - " + endFormat + "]: " + formerTitle + "\n"
	params := url.Values{}
	params.Add("term", formerTitle)

	bandCampParams := url.Values{}
	bandCampParams.Add("q", formerTitle)

	notes += "<a href=\"https://music.apple.com/ca/search?" + params.Encode() + "\">Apple Music</a> | "
	notes += "<a href=\"https://bandcamp.com/search?" + bandCampParams.Encode() + "\">Bandcamp</a><br /> "
	metadataFile, err := writeTempTextFile("gopod-metadata-*.txt", fileContent)
	if err != nil {
		return "", "", err
	}
	descriptionFile, err := writeTempTextFile("gopod-description-*.txt", notes)
	if err != nil {
		os.Remove(metadataFile)
		return "", "", err
	}

	return metadataFile, descriptionFile, nil
}

func formatChapterRange(chapterStart, chapterEnd int) (string, string) {
	var zero time.Time
	start := zero.Add(time.Duration(chapterStart) * time.Second)
	end := zero.Add(time.Duration(chapterEnd) * time.Second)
	return start.Format("15:04:05"), end.Format("15:04:05")
}

func writeTempTextFile(pattern, content string) (path string, err error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path = f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(path)
		}
	}()

	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}
