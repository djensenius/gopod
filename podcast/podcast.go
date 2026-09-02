package podcast

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eduncan911/podcast"
)

// Podcasts : Array of podcasts
type Podcasts struct {
	Podcasts []*Podcast
}

// Podcast : Details of podcast
type Podcast struct {
	Title      string
	ShortTitle string
	Directory  string
	Image      string
	URL        string
	PodcastURL string
	SourceURL  string
	Extension  string
	Length     int
}

const (
	reservedWorkFilePrefix            = ".gopod-"
	publicationOwnershipPrefix        = reservedWorkFilePrefix + "owner-"
	publicationCompletionMarkerPrefix = reservedWorkFilePrefix + "complete-"
	publicationCompletionStagePrefix  = reservedWorkFilePrefix + "commit-"
	publicationVisibilityGuardPrefix  = reservedWorkFilePrefix + "guard-"
	publicationVisibilityMarkerPrefix = reservedWorkFilePrefix + "visible-"
	publicationControlFileMode        = 0o600
	publicationIDSize                 = 16
	publicationIDTextLength           = publicationIDSize * 2
	publicationProtocol               = "gopod1"
)

func ValidateShortTitle(shortTitle string) error {
	switch {
	case shortTitle == "":
		return errors.New("short title must not be empty")
	case shortTitle == "." || shortTitle == "..":
		return fmt.Errorf("short title %q is not allowed", shortTitle)
	case filepath.Base(shortTitle) != shortTitle:
		return fmt.Errorf("short title %q must not contain path separators", shortTitle)
	case strings.HasPrefix(shortTitle+"-", reservedWorkFilePrefix):
		return fmt.Errorf(
			"short title %q would create output filenames with reserved prefix %q",
			shortTitle,
			reservedWorkFilePrefix,
		)
	default:
		return nil
	}
}

// Event : Filesystem change data
type Event struct {
	Path  string
	Event string
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

func FindPodcast(shortName string, p Podcasts) (Podcast, error) {
	podcast := Podcast{}
	found := false
	for _, pc := range p.Podcasts {
		if pc.ShortTitle == shortName {
			podcast = *pc
			found = true
		}
	}
	if !found {
		return podcast, errors.New("Podcast not found")
	}
	return podcast, nil
}

func GeneratePodcastFeed(podcastData Podcast) error {
	fullDir := podcastData.Directory
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return fmt.Errorf("read podcast directory %q: %w", fullDir, err)
	}

	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), reservedWorkFilePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read podcast file info %q: %w", entry.Name(), err)
		}
		files = append(files, info)
	}
	sort.Sort(ByModTime(files))
	pubDate := time.Now()
	updatedDate := time.Now()

	feed := podcast.New(
		podcastData.Title,
		podcastData.PodcastURL,
		podcastData.Title,
		&pubDate, &updatedDate,
	)
	feed.AddImage(podcastData.Image)

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".mp3" || filepath.Ext(file.Name()) == ".aac" || filepath.Ext(file.Name()) == ".m4a" {
			stem := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
			if _, _, generated := parseGeneratedPublicationStem(stem); generated {
				committed, err := hasPublicationCompletionMarker(fullDir, stem)
				if err != nil {
					return err
				}
				if !committed {
					continue
				}
			}

			podcastTime := file.ModTime()
			title := podcastData.Title + " " + file.ModTime().Format(time.RFC850)
			var podcastDownload strings.Builder
			podcastDownload.WriteString(podcastData.URL)
			podcastDownload.WriteString(file.Name())

			description := "📻🤖"
			descriptionFile := stem + ".txt"
			data, err := os.ReadFile(filepath.Join(fullDir, descriptionFile))
			if err == nil {
				description = string(data)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read podcast description %q: %w", descriptionFile, err)
			}

			item := podcast.Item{
				Title:       title,
				Link:        podcastDownload.String(),
				Description: description,
				PubDate:     &podcastTime,
			}
			item.AddImage(podcastData.Image)
			if filepath.Ext(file.Name()) == ".mp3" {
				item.AddEnclosure(podcastDownload.String(), podcast.MP3, file.Size())
			} else {
				item.AddEnclosure(podcastDownload.String(), podcast.M4A, file.Size())
			}
			if _, err := feed.AddItem(item); err != nil {
				return fmt.Errorf("add podcast item %q: %w", file.Name(), err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(fullDir, "podcast.rss"), feed.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write podcast feed: %w", err)
	}
	return nil
}

func publicationOwnershipPath(directory, stem string) string {
	return filepath.Join(
		directory,
		publicationOwnershipPrefix+publicationInternalKey(stem),
	)
}

func publicationCompletionMarkerPath(directory, stem string) string {
	return filepath.Join(
		directory,
		publicationCompletionMarkerPrefix+publicationInternalKey(stem),
	)
}

func publicationCompletionStagePath(directory, stem string) string {
	return filepath.Join(
		directory,
		publicationCompletionStagePrefix+publicationInternalKey(stem),
	)
}

func publicationVisibilityGuardPath(directory, stem string) string {
	return filepath.Join(
		directory,
		publicationVisibilityGuardPrefix+publicationInternalKey(stem),
	)
}

func publicationVisibilityMarkerPath(directory, stem string) string {
	return filepath.Join(
		directory,
		publicationVisibilityMarkerPrefix+publicationInternalKey(stem),
	)
}

func publicationInternalKey(stem string) string {
	_, unixNano, tokenText, ok := parseGeneratedPublicationStemParts(stem)
	if !ok {
		panic("publication control path requested for a non-versioned stem")
	}
	keyMaterial := strconv.FormatInt(unixNano, 10) +
		"\x00" + publicationProtocol +
		"\x00" + tokenText
	sum := sha256.Sum256([]byte(keyMaterial))
	return hex.EncodeToString(sum[:])
}

func hasPublicationCompletionMarker(directory, stem string) (bool, error) {
	stagePath := publicationCompletionStagePath(directory, stem)
	guardPath := publicationVisibilityGuardPath(directory, stem)
	controlsAbsent, err := publicationVisibilityControlsAbsent(
		stagePath,
		guardPath,
	)
	if err != nil || !controlsAbsent {
		return false, err
	}

	ownershipID, valid, err := readPublicationIDFile(
		publicationOwnershipPath(directory, stem),
		"ownership",
	)
	if err != nil || !valid {
		return false, err
	}
	completionID, valid, err := readPublicationIDFile(
		publicationCompletionMarkerPath(directory, stem),
		"completion marker",
	)
	if err != nil || !valid {
		return false, err
	}
	visibilityID, valid, err := readPublicationIDFile(
		publicationVisibilityMarkerPath(directory, stem),
		"visibility marker",
	)
	if err != nil || !valid {
		return false, err
	}
	if ownershipID != completionID || ownershipID != visibilityID {
		return false, nil
	}
	return publicationVisibilityControlsAbsent(stagePath, guardPath)
}

func publicationVisibilityControlsAbsent(
	completionStagePath string,
	visibilityGuardPath string,
) (bool, error) {
	stageAbsent, err := publicationControlAbsent(
		completionStagePath,
		"commit stage",
	)
	if err != nil || !stageAbsent {
		return false, err
	}
	return publicationControlAbsent(visibilityGuardPath, "visibility guard")
}

func publicationControlAbsent(path string, label string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf(
			"inspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	return true, nil
}

func readPublicationIDFile(
	path string,
	label string,
) (publicationID string, valid bool, err error) {
	pathIdentity, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf(
			"inspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !pathIdentity.Mode().IsRegular() ||
		pathIdentity.Size() != publicationIDTextLength {
		return "", false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf(
			"open podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf(
					"close podcast publication %s %q: %w",
					label,
					path,
					closeErr,
				),
			)
		}
	}()

	openedIdentity, err := file.Stat()
	if err != nil {
		return "", false, fmt.Errorf(
			"inspect open podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !openedIdentity.Mode().IsRegular() ||
		openedIdentity.Size() != publicationIDTextLength ||
		!os.SameFile(pathIdentity, openedIdentity) {
		return "", false, nil
	}

	data, err := io.ReadAll(io.LimitReader(file, publicationIDTextLength+1))
	if err != nil {
		return "", false, fmt.Errorf(
			"read podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !validPublicationID(data) {
		return "", false, nil
	}
	currentIdentity, err := os.Lstat(path)
	if err != nil {
		return "", false, fmt.Errorf(
			"reinspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !os.SameFile(pathIdentity, currentIdentity) {
		return "", false, nil
	}
	return string(data), true, nil
}

func validPublicationID(data []byte) bool {
	if len(data) != publicationIDTextLength {
		return false
	}
	for _, character := range data {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func parseGeneratedPublicationStem(
	stem string,
) (shortTitle string, unixNano int64, ok bool) {
	shortTitle, unixNano, _, ok = parseGeneratedPublicationStemParts(stem)
	return shortTitle, unixNano, ok
}

func parseGeneratedPublicationStemParts(
	stem string,
) (shortTitle string, unixNano int64, tokenText string, ok bool) {
	tokenSeparator := strings.LastIndexByte(stem, '-')
	if tokenSeparator <= 0 || tokenSeparator == len(stem)-1 {
		return "", 0, "", false
	}
	tokenText = stem[tokenSeparator+1:]
	if len(tokenText) != hex.EncodedLen(publicationTokenSize) {
		return "", 0, "", false
	}
	token := make([]byte, publicationTokenSize)
	if _, err := hex.Decode(token, []byte(tokenText)); err != nil {
		return "", 0, "", false
	}
	if hex.EncodeToString(token) != tokenText {
		return "", 0, "", false
	}

	protocolTimestampedTitle := stem[:tokenSeparator]
	protocolSeparator := strings.LastIndexByte(protocolTimestampedTitle, '-')
	if protocolSeparator <= 0 ||
		protocolSeparator == len(protocolTimestampedTitle)-1 ||
		protocolTimestampedTitle[protocolSeparator+1:] != publicationProtocol {
		return "", 0, "", false
	}

	timestampedTitle := protocolTimestampedTitle[:protocolSeparator]
	timestampSeparator := strings.LastIndexByte(timestampedTitle, '-')
	if timestampSeparator <= 0 || timestampSeparator == len(timestampedTitle)-1 {
		return "", 0, "", false
	}
	shortTitle = timestampedTitle[:timestampSeparator]
	if err := ValidateShortTitle(shortTitle); err != nil {
		return "", 0, "", false
	}

	timestampText := timestampedTitle[timestampSeparator+1:]
	unixNano, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil || unixNano < 0 ||
		strconv.FormatInt(unixNano, 10) != timestampText {
		return "", 0, "", false
	}
	return shortTitle, unixNano, tokenText, true
}
