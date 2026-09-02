//go:build windows

package podcast

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

func syncPublicationDirectory(path string) (err error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf(
			"encode publication directory %q for sync: %w",
			path,
			err,
		)
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return fmt.Errorf("open publication directory %q for sync: %w", path, err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
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

	if err := windows.FlushFileBuffers(handle); err != nil {
		return fmt.Errorf("sync publication directory %q: %w", path, err)
	}
	return nil
}
