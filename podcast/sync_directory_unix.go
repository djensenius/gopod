//go:build linux || darwin

package podcast

import (
	"errors"
	"fmt"
	"os"
)

func syncPublicationDirectory(path string) (err error) {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open publication directory %q for sync: %w", path, err)
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf(
					"close publication directory %q after sync: %w",
					path,
					closeErr,
				),
			)
		}
	}()

	info, err := directory.Stat()
	if err != nil {
		return fmt.Errorf(
			"inspect publication directory %q before sync: %w",
			path,
			err,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf("publication directory %q is not a directory", path)
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync publication directory %q: %w", path, err)
	}
	return nil
}
