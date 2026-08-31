//go:build linux

package podcast

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type linuxRenameOperations struct {
	renameNoReplace func(oldPath, newPath string) error
	link            func(oldPath, newPath string) error
	remove          func(path string) error
	lstat           func(path string) (os.FileInfo, error)
	sameFile        func(first, second os.FileInfo) bool
}

func renameNoReplace(oldPath, newPath string) error {
	return renameNoReplaceLinux(oldPath, newPath, linuxRenameOperations{
		renameNoReplace: func(oldPath, newPath string) error {
			return unix.Renameat2(
				unix.AT_FDCWD,
				oldPath,
				unix.AT_FDCWD,
				newPath,
				unix.RENAME_NOREPLACE,
			)
		},
		link:     os.Link,
		remove:   os.Remove,
		lstat:    os.Lstat,
		sameFile: os.SameFile,
	})
}

func renameNoReplaceLinux(
	oldPath string,
	newPath string,
	operations linuxRenameOperations,
) error {
	renameErr := operations.renameNoReplace(oldPath, newPath)
	if renameErr == nil {
		return nil
	}
	if errors.Is(renameErr, os.ErrExist) {
		return os.ErrExist
	}
	if !linuxNoReplaceRenameUnsupported(renameErr) {
		return renameErr
	}
	if filepath.Clean(filepath.Dir(oldPath)) != filepath.Clean(filepath.Dir(newPath)) {
		return fmt.Errorf(
			"atomic no-clobber fallback requires staging and destination in the same directory: %q and %q",
			oldPath,
			newPath,
		)
	}

	sourceIdentity, err := operations.lstat(oldPath)
	if err != nil {
		return fmt.Errorf(
			"inspect staging file %q for hard-link publication fallback: %w",
			oldPath,
			err,
		)
	}
	if linkErr := operations.link(oldPath, newPath); linkErr != nil {
		if errors.Is(linkErr, os.ErrExist) {
			return os.ErrExist
		}
		if linuxHardLinkUnsupported(linkErr) {
			return fmt.Errorf(
				"filesystem supports neither renameat2 RENAME_NOREPLACE nor same-filesystem hard-link publication; use storage that supports at least one: %w",
				linkErr,
			)
		}
		return fmt.Errorf(
			"renameat2 RENAME_NOREPLACE is unsupported and hard-link publication %q to %q failed: %w",
			oldPath,
			newPath,
			linkErr,
		)
	}

	removeErr := operations.remove(oldPath)
	if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
		return nil
	}
	rollbackErr := rollbackLinuxLinkedDestination(
		newPath,
		sourceIdentity,
		operations,
	)
	if rollbackErr != nil {
		return errors.Join(
			fmt.Errorf(
				"remove staging file %q after hard-link publication: %w",
				oldPath,
				removeErr,
			),
			fmt.Errorf(
				"roll back linked destination %q: %w",
				newPath,
				rollbackErr,
			),
		)
	}
	return fmt.Errorf(
		"remove staging file %q after hard-link publication; destination rollback succeeded: %w",
		oldPath,
		removeErr,
	)
}

func rollbackLinuxLinkedDestination(
	path string,
	sourceIdentity os.FileInfo,
	operations linuxRenameOperations,
) error {
	destinationIdentity, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect linked destination: %w", err)
	}
	if !operations.sameFile(sourceIdentity, destinationIdentity) {
		return errors.New(
			"refuse to remove destination because it no longer refers to the staging file",
		)
	}
	if err := operations.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove linked destination: %w", err)
	}
	return nil
}

func linuxNoReplaceRenameUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.EOPNOTSUPP)
}

func linuxHardLinkUnsupported(err error) bool {
	return errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.EOPNOTSUPP)
}
