package podcast

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Record(ctx context.Context, podcast Podcast) (recordingPath string, err error) {
	extension := strings.TrimPrefix(podcast.Extension, ".")
	if extension == "" || filepath.Base(extension) != extension {
		return "", fmt.Errorf("invalid recording extension %q", podcast.Extension)
	}

	f, err := os.CreateTemp("", "gopod-recording-*."+extension)
	if err != nil {
		return "", fmt.Errorf("create temporary recording: %w", err)
	}
	tmpFile := f.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, removeOwnedFile(tmpFile))
		}
	}()
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temporary recording %q: %w", tmpFile, err)
	}

	var t time.Time
	t = t.Add(time.Duration(podcast.Length) * time.Second)
	tFormat := t.Format("15:04:05")
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-nostdin",
		"-y",
		"-i", podcast.SourceURL,
		"-t", tFormat,
		"-c", "copy",
		"-map_metadata", "0",
		tmpFile,
	)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("run ffmpeg recorder: %w", ctxErr)
		}
		return "", fmt.Errorf("run ffmpeg recorder: %w", err)
	}
	return tmpFile, nil
}

func Combine(
	ctx context.Context,
	podcast Podcast,
	audioFile string,
	metaFile string,
	descriptionFile string,
) (err error) {
	audioStage, err := createClosedTempFile(
		podcast.Directory,
		".gopod-audio-*.part",
	)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, removeOwnedFile(audioStage))
	}()

	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-nostdin",
		"-y",
		"-i", audioFile,
		"-i", metaFile,
		"-map_metadata", "1",
		"-c:a", "aac",
		"-f", "ipod",
		audioStage,
	)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("run ffmpeg combiner: %w", ctxErr)
		}
		return fmt.Errorf("run ffmpeg combiner: %w", err)
	}
	if err := os.Chmod(audioStage, 0o644); err != nil {
		return fmt.Errorf("set podcast audio permissions %q: %w", audioStage, err)
	}

	descriptionStage := ""
	if descriptionFile != "" {
		data, err := os.ReadFile(descriptionFile)
		if err != nil {
			return fmt.Errorf("read podcast description: %w", err)
		}
		descriptionStage, err = createClosedTempFile(
			podcast.Directory,
			".gopod-description-*.part",
		)
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, removeOwnedFile(descriptionStage))
		}()
		if err := os.WriteFile(descriptionStage, data, 0o644); err != nil {
			return fmt.Errorf("write temporary podcast description %q: %w", descriptionStage, err)
		}
		if err := os.Chmod(descriptionStage, 0o644); err != nil {
			return fmt.Errorf(
				"set podcast description permissions %q: %w",
				descriptionStage,
				err,
			)
		}
	}

	if _, _, err := publishCompletedRecording(
		podcast,
		audioStage,
		descriptionStage,
		time.Now(),
	); err != nil {
		return err
	}
	return nil
}

func createClosedTempFile(directory, pattern string) (path string, err error) {
	f, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary podcast file in %q: %w", directory, err)
	}
	path = f.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close(), removeOwnedFile(path))
		}
	}()
	if closeErr := f.Close(); closeErr != nil {
		err = fmt.Errorf("close temporary podcast file %q: %w", path, closeErr)
		return
	}
	return path, nil
}

func publishCompletedRecording(
	podcast Podcast,
	audioStage string,
	descriptionStage string,
	recordedAt time.Time,
) (audioPath string, descriptionPath string, err error) {
	return publishCompletedRecordingWith(
		podcast,
		audioStage,
		descriptionStage,
		recordedAt,
		rand.Reader,
		renameNoReplace,
	)
}

type renameFileFunc func(oldPath, newPath string) error

func publishCompletedRecordingWith(
	podcast Podcast,
	audioStage string,
	descriptionStage string,
	recordedAt time.Time,
	random io.Reader,
	renameFile renameFileFunc,
) (audioPath string, descriptionPath string, err error) {
	const (
		maxAttempts          = 100
		publicationTokenSize = 16
	)

	if err := ValidateShortTitle(podcast.ShortTitle); err != nil {
		return "", "", fmt.Errorf(
			"invalid podcast short title for output filename: %w",
			err,
		)
	}

	token := make([]byte, publicationTokenSize)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if _, err := io.ReadFull(random, token); err != nil {
			return "", "", fmt.Errorf("generate unique podcast output name: %w", err)
		}
		stem := publicationStem(podcast.ShortTitle, recordedAt, token)
		audioPath = filepath.Join(podcast.Directory, stem+".m4a")
		descriptionPath = ""
		if descriptionStage != "" {
			descriptionPath = filepath.Join(podcast.Directory, stem+".txt")
		}

		unused, err := publicationPathsUnused(audioPath, descriptionPath)
		if err != nil {
			return "", "", err
		}
		if !unused {
			continue
		}

		if err := publishStagedRecording(
			audioStage,
			descriptionStage,
			audioPath,
			descriptionPath,
			renameFile,
		); err != nil {
			return "", "", err
		}
		return audioPath, descriptionPath, nil
	}

	return "", "", fmt.Errorf(
		"publish podcast %q: exhausted %d unique output names",
		podcast.ShortTitle,
		maxAttempts,
	)
}

func publicationStem(shortTitle string, recordedAt time.Time, token []byte) string {
	return fmt.Sprintf(
		"%s-%d-%s",
		shortTitle,
		recordedAt.UnixNano(),
		hex.EncodeToString(token),
	)
}

func publicationPathsUnused(paths ...string) (bool, error) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("check podcast output path %q: %w", path, err)
		}
	}
	return true, nil
}

func publishStagedRecording(
	audioStage string,
	descriptionStage string,
	audioPath string,
	descriptionPath string,
	renameFile renameFileFunc,
) (err error) {
	descriptionOwned := false
	var descriptionIdentity os.FileInfo
	defer func() {
		if err != nil && descriptionOwned {
			err = errors.Join(
				err,
				removeOwnedPublishedFile(descriptionPath, descriptionIdentity),
			)
		}
	}()

	if descriptionStage != "" {
		descriptionIdentity, err = os.Lstat(descriptionStage)
		if err != nil {
			return fmt.Errorf(
				"inspect temporary podcast description %q: %w",
				descriptionStage,
				err,
			)
		}
		if err := renameFile(descriptionStage, descriptionPath); err != nil {
			return fmt.Errorf(
				"publish podcast description %q: %w",
				descriptionPath,
				err,
			)
		}
		descriptionOwned = true
	}
	if err := renameFile(audioStage, audioPath); err != nil {
		return fmt.Errorf("publish podcast audio %q: %w", audioPath, err)
	}
	return nil
}

func removeOwnedPublishedFile(path string, identity os.FileInfo) error {
	currentIdentity, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect owned published file %q: %w", path, err)
	}
	if !os.SameFile(identity, currentIdentity) {
		return fmt.Errorf(
			"refuse to remove published file %q because it no longer refers to this invocation's file",
			path,
		)
	}
	return removeOwnedFile(path)
}

func removeOwnedFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove owned temporary file %q: %w", path, err)
	}
	return nil
}
