//go:build !linux && !darwin && !windows

package podcast

import (
	"fmt"
	"runtime"
)

func renameNoReplace(_, _ string) error {
	return fmt.Errorf(
		"atomic no-replace podcast publication is unsupported on %s",
		runtime.GOOS,
	)
}
