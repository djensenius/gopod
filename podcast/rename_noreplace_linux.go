//go:build linux

package podcast

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type linuxRenameOperations struct {
	renameNoReplace  func(oldPath, newPath string) error
	restoreNoReplace func(oldPath, newPath string) error
	rename           func(oldPath, newPath string) error
	link             func(oldPath, newPath string) error
	remove           func(path string) error
	lstat            func(path string) (os.FileInfo, error)
	sameFile         func(first, second os.FileInfo) bool
	warn             func(error)
}

func renameNoReplace(oldPath, newPath string) error {
	return renameNoReplaceLinux(oldPath, newPath, defaultLinuxRenameOperations())
}

func publishVisibilityMarker(
	oldPath string,
	newPath string,
	expectedIdentity os.FileInfo,
) error {
	return publishVisibilityMarkerLinux(
		oldPath,
		newPath,
		expectedIdentity,
		defaultLinuxRenameOperations(),
	)
}

func defaultLinuxRenameOperations() linuxRenameOperations {
	return linuxRenameOperations{
		renameNoReplace:  renameNoReplaceLinuxSyscall,
		restoreNoReplace: restoreNoReplace,
		rename:           os.Rename,
		link:             os.Link,
		remove:           os.Remove,
		lstat:            os.Lstat,
		sameFile:         os.SameFile,
		warn: func(err error) {
			log.Printf("podcast publication warning: %v", err)
		},
	}
}

func renameNoReplaceLinuxSyscall(oldPath, newPath string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		oldPath,
		unix.AT_FDCWD,
		newPath,
		unix.RENAME_NOREPLACE,
	)
}

type linuxRestoreOperations struct {
	renameNoReplace func(oldPath, newPath string) error
	link            func(oldPath, newPath string) error
	remove          func(path string) error
}

func restoreNoReplace(oldPath, newPath string) error {
	return restoreNoReplaceLinux(oldPath, newPath, linuxRestoreOperations{
		renameNoReplace: renameNoReplaceLinuxSyscall,
		link:            os.Link,
		remove:          os.Remove,
	})
}

func restoreNoReplaceLinux(
	oldPath string,
	newPath string,
	operations linuxRestoreOperations,
) error {
	renameErr := operations.renameNoReplace(oldPath, newPath)
	if renameErr == nil {
		return nil
	}
	return finishRestoreNoReplaceLinux(
		oldPath,
		newPath,
		operations,
		renameErr,
	)
}

func publishVisibilityMarkerLinux(
	oldPath string,
	newPath string,
	expectedIdentity os.FileInfo,
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
			"atomic visibility fallback requires guard and marker in the same directory: %q and %q",
			oldPath,
			newPath,
		)
	}

	sourceIdentity, err := operations.lstat(oldPath)
	if err != nil {
		return fmt.Errorf(
			"inspect visibility guard %q for hard-link publication fallback: %w",
			oldPath,
			err,
		)
	}
	if !operations.sameFile(expectedIdentity, sourceIdentity) {
		return fmt.Errorf(
			"visibility guard %q ownership changed before publication",
			oldPath,
		)
	}
	if err := operations.link(oldPath, newPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		if linuxHardLinkUnsupported(err) {
			return fmt.Errorf(
				"filesystem supports neither renameat2 RENAME_NOREPLACE nor same-filesystem hard-link visibility publication; use storage that supports at least one: %w",
				err,
			)
		}
		return fmt.Errorf(
			"hard-link visibility guard %q to marker %q: %w",
			oldPath,
			newPath,
			err,
		)
	}

	markerIdentity, err := operations.lstat(newPath)
	if err != nil || !operations.sameFile(expectedIdentity, markerIdentity) {
		verificationErr := fmt.Errorf(
			"verify hard-linked visibility marker %q",
			newPath,
		)
		if err != nil {
			verificationErr = fmt.Errorf("%w: %v", verificationErr, err)
		}
		rollbackResult := rollbackLinuxLinkedDestination(
			newPath,
			expectedIdentity,
			operations,
		)
		rollbackErr := finalizeLinuxOwnedFileCleanup(
			rollbackResult,
			"visibility marker rollback",
			operations,
		)
		if rollbackErr != nil {
			return errors.Join(
				verificationErr,
				fmt.Errorf(
					"roll back unverified visibility marker %q: %w",
					newPath,
					rollbackErr,
				),
			)
		}
		return fmt.Errorf(
			"%w; visibility marker rollback succeeded",
			verificationErr,
		)
	}

	cleanupDirectory, err := os.MkdirTemp(
		filepath.Dir(oldPath),
		ownedFileCleanupDirectoryPattern,
	)
	if err != nil {
		rollbackResult := rollbackLinuxLinkedDestination(
			newPath,
			expectedIdentity,
			operations,
		)
		rollbackErr := finalizeLinuxOwnedFileCleanup(
			rollbackResult,
			"visibility marker rollback",
			operations,
		)
		return errors.Join(
			fmt.Errorf(
				"create private cleanup directory for visibility guard %q: %w",
				oldPath,
				err,
			),
			rollbackErr,
		)
	}
	claimedPath := filepath.Join(cleanupDirectory, ownedFileCleanupClaimName)
	if claimErr := operations.rename(oldPath, claimedPath); claimErr != nil {
		canonicalIdentity, canonicalErr := operations.lstat(oldPath)
		if canonicalErr == nil &&
			operations.sameFile(expectedIdentity, canonicalIdentity) {
			directoryErr := removeEmptyOwnedFileCleanupDirectory(
				cleanupDirectory,
				operations.ownedFileCleanupOperations(),
			)
			rollbackResult := rollbackLinuxLinkedDestination(
				newPath,
				expectedIdentity,
				operations,
			)
			rollbackErr := finalizeLinuxOwnedFileCleanup(
				rollbackResult,
				"visibility marker rollback",
				operations,
			)
			return errors.Join(
				fmt.Errorf(
					"atomically claim visibility guard %q after marker publication: %w",
					oldPath,
					claimErr,
				),
				directoryErr,
				rollbackErr,
			)
		}

		claimedIdentity, claimedErr := operations.lstat(claimedPath)
		if claimedErr == nil &&
			operations.sameFile(expectedIdentity, claimedIdentity) {
			warnLinuxPublication(
				operations,
				fmt.Errorf(
					"visibility guard claim %q completed despite an error response: %w",
					claimedPath,
					claimErr,
				),
			)
		} else {
			warnLinuxPublication(
				operations,
				errors.Join(
					fmt.Errorf(
						"visibility guard claim %q has an irreversible outcome after marker publication: %w",
						claimedPath,
						claimErr,
					),
					canonicalErr,
					claimedErr,
				),
			)
			return nil
		}
	}

	// The canonical guard is now absent and the verified marker is present.
	// From this point the visibility commit is irreversible.
	claimedIdentity, inspectErr := operations.lstat(claimedPath)
	if inspectErr != nil {
		warnLinuxPublication(
			operations,
			fmt.Errorf(
				"inspect committed visibility guard claim %q: %w",
				claimedPath,
				inspectErr,
			),
		)
		return nil
	}
	if !operations.sameFile(expectedIdentity, claimedIdentity) {
		warnLinuxPublication(
			operations,
			fmt.Errorf(
				"retain replacement visibility guard at %q after commit because its identity changed",
				claimedPath,
			),
		)
		return nil
	}
	if err := operations.remove(claimedPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		warnLinuxPublication(
			operations,
			fmt.Errorf(
				"remove committed visibility guard claim %q: %w",
				claimedPath,
				err,
			),
		)
		return nil
	}
	warnLinuxPublication(
		operations,
		removeEmptyOwnedFileCleanupDirectory(
			cleanupDirectory,
			operations.ownedFileCleanupOperations(),
		),
	)
	return nil
}

func warnLinuxPublication(operations linuxRenameOperations, err error) {
	if err != nil && operations.warn != nil {
		operations.warn(err)
	}
}

func finishRestoreNoReplaceLinux(
	oldPath string,
	newPath string,
	operations linuxRestoreOperations,
	renameErr error,
) error {
	if errors.Is(renameErr, os.ErrExist) {
		return os.ErrExist
	}
	if !linuxNoReplaceRenameUnsupported(renameErr) {
		return renameErr
	}

	if err := operations.link(oldPath, newPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return fmt.Errorf(
			"restore %q to %q with a no-clobber hard link: %w",
			oldPath,
			newPath,
			err,
		)
	}
	if err := operations.remove(oldPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"remove private cleanup claim %q after restoring %q: %w",
			oldPath,
			newPath,
			err,
		)
	}
	return nil
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
	linkedIdentity, err := operations.lstat(newPath)
	if err != nil {
		verificationErr := fmt.Errorf(
			"inspect hard-link publication destination %q: %w",
			newPath,
			err,
		)
		rollbackResult := rollbackLinuxLinkedDestination(
			newPath,
			sourceIdentity,
			operations,
		)
		rollbackErr := finalizeLinuxOwnedFileCleanup(
			rollbackResult,
			"hard-link destination rollback",
			operations,
		)
		if rollbackErr != nil {
			return errors.Join(
				verificationErr,
				fmt.Errorf(
					"roll back unverified linked destination %q: %w",
					newPath,
					rollbackErr,
				),
			)
		}
		return fmt.Errorf(
			"%w; unverified linked destination rollback succeeded",
			verificationErr,
		)
	}
	if !operations.sameFile(sourceIdentity, linkedIdentity) {
		sourceAfterLink, sourceErr := operations.lstat(oldPath)
		verificationErr := fmt.Errorf(
			"staging file %q changed before hard-link publication",
			oldPath,
		)
		if sourceErr != nil ||
			!operations.sameFile(sourceAfterLink, linkedIdentity) {
			if sourceErr != nil {
				return errors.Join(
					verificationErr,
					fmt.Errorf(
						"cannot safely identify the linked replacement at %q because the current source cannot be inspected: %w",
						newPath,
						sourceErr,
					),
				)
			}
			return errors.Join(
				verificationErr,
				fmt.Errorf(
					"cannot safely roll back linked destination %q because it no longer matches the current source",
					newPath,
				),
			)
		}
		rollbackResult := rollbackLinuxLinkedDestination(
			newPath,
			linkedIdentity,
			operations,
		)
		rollbackErr := finalizeLinuxOwnedFileCleanup(
			rollbackResult,
			"linked replacement destination rollback",
			operations,
		)
		if rollbackErr != nil {
			return errors.Join(
				verificationErr,
				fmt.Errorf(
					"roll back linked replacement destination %q: %w",
					newPath,
					rollbackErr,
				),
			)
		}
		return fmt.Errorf(
			"%w; linked replacement destination rollback succeeded",
			verificationErr,
		)
	}

	cleanupResult := removeOwnedFileWithIdentityUsingResult(
		oldPath,
		sourceIdentity,
		"hard-link source",
		operations.ownedFileCleanupOperations(),
	)
	if cleanupResult.fileRemoved {
		_ = finalizeLinuxOwnedFileCleanup(
			cleanupResult,
			"hard-link source cleanup",
			operations,
		)
		return nil
	}
	cleanupErr := cleanupResult.combinedError()
	if !cleanupResult.fileRetained {
		return fmt.Errorf(
			"remove hard-link publication source %q safely; linked destination %q was preserved because the source removal outcome is unknown: %w",
			oldPath,
			newPath,
			cleanupErr,
		)
	}
	rollbackResult := rollbackLinuxLinkedDestination(
		newPath,
		sourceIdentity,
		operations,
	)
	rollbackErr := finalizeLinuxOwnedFileCleanup(
		rollbackResult,
		"linked destination rollback",
		operations,
	)
	if rollbackErr != nil {
		return errors.Join(
			fmt.Errorf(
				"remove hard-link publication source %q safely: %w",
				oldPath,
				cleanupErr,
			),
			fmt.Errorf(
				"roll back linked destination %q: %w",
				newPath,
				rollbackErr,
			),
		)
	}
	return fmt.Errorf(
		"remove hard-link publication source %q safely; destination rollback succeeded: %w",
		oldPath,
		cleanupErr,
	)
}

func (operations linuxRenameOperations) ownedFileCleanupOperations() ownedFileCleanupOperations {
	return ownedFileCleanupOperations{
		rename:          operations.rename,
		renameNoReplace: operations.restoreNoReplace,
		remove:          operations.remove,
		lstat:           operations.lstat,
		sameFile:        operations.sameFile,
	}
}

func rollbackLinuxLinkedDestination(
	path string,
	sourceIdentity os.FileInfo,
	operations linuxRenameOperations,
) ownedFileCleanupResult {
	return removeOwnedFileWithIdentityUsingResult(
		path,
		sourceIdentity,
		"linked destination",
		operations.ownedFileCleanupOperations(),
	)
}

func finalizeLinuxOwnedFileCleanup(
	result ownedFileCleanupResult,
	context string,
	operations linuxRenameOperations,
) error {
	if !result.fileRemoved {
		return result.combinedError()
	}
	if cleanupErr := result.combinedError(); cleanupErr != nil &&
		operations.warn != nil {
		operations.warn(fmt.Errorf("%s: %w", context, cleanupErr))
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
