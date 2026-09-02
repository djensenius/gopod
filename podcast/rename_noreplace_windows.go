//go:build windows

package podcast

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func renameNoReplace(oldPath, newPath string) error {
	oldPathPointer, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPointer, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(
		oldPathPointer,
		newPathPointer,
		windows.MOVEFILE_WRITE_THROUGH,
	)
	if errors.Is(err, windows.ERROR_FILE_EXISTS) ||
		errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return os.ErrExist
	}
	return err
}

func restoreNoReplace(oldPath, newPath string) error {
	return renameNoReplace(oldPath, newPath)
}

func publishVisibilityMarker(
	oldPath string,
	newPath string,
	_ os.FileInfo,
) error {
	return renameNoReplace(oldPath, newPath)
}
