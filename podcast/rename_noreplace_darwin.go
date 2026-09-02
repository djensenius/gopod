//go:build darwin

package podcast

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldPath, newPath string) error {
	return unix.RenamexNp(oldPath, newPath, unix.RENAME_EXCL)
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
