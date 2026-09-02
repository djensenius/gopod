//go:build !linux && !darwin && !windows

package podcast

import (
	"fmt"
	"os"
	"runtime"
)

func renameNoReplace(_, _ string) error {
	return fmt.Errorf(
		"atomic no-replace podcast publication is unsupported on %s",
		runtime.GOOS,
	)
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
