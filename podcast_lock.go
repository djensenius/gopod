package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopod/podcast"
)

var (
	errFileLockHeld  = errors.New("file lock is held")
	errPodcastLocked = errors.New("podcast is already running")
)

type podcastLock struct {
	file *os.File
	path string
}

func acquirePodcastLock(selected podcast.Podcast) (*podcastLock, error) {
	path := podcastLockPath(selected)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf(
			"open lock for podcast %q at %q: %w",
			selected.ShortTitle,
			path,
			err,
		)
	}

	if err := tryLockFile(file); err != nil {
		closeErr := file.Close()
		if errors.Is(err, errFileLockHeld) {
			return nil, errors.Join(
				fmt.Errorf(
					"%w: podcast %q is locked by another process at %q",
					errPodcastLocked,
					selected.ShortTitle,
					path,
				),
				closeErr,
			)
		}
		return nil, errors.Join(
			fmt.Errorf(
				"lock podcast %q at %q: %w",
				selected.ShortTitle,
				path,
				err,
			),
			closeErr,
		)
	}

	return &podcastLock{file: file, path: path}, nil
}

func podcastLockPath(selected podcast.Podcast) string {
	sum := sha256.Sum256([]byte(selected.ShortTitle))
	return filepath.Join(
		selected.Directory,
		fmt.Sprintf(".gopod-%x.lock", sum[:8]),
	)
}

func (lock *podcastLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockFile(lock.file)
	closeErr := lock.file.Close()
	lock.file = nil
	return errors.Join(unlockErr, closeErr)
}
