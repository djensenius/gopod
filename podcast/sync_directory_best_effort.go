//go:build !linux && !darwin && !windows

package podcast

import (
	"fmt"
	"os"
)

func syncPublicationDirectory(path string) error {
	// Windows and the remaining supported targets do not expose a portable
	// directory-entry flush with Go's documented guarantees. Public and control
	// files are still synced individually; this check reports inaccessible or
	// replaced publication directories without claiming metadata durability.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf(
			"inspect publication directory %q for best-effort sync: %w",
			path,
			err,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf("publication directory %q is not a directory", path)
	}
	return nil
}
