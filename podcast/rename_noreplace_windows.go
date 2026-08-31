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
	err = windows.MoveFile(oldPathPointer, newPathPointer)
	if errors.Is(err, windows.ERROR_FILE_EXISTS) ||
		errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return os.ErrExist
	}
	return err
}
