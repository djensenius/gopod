//go:build linux

package podcast

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRestoreNoReplaceLinuxFallsBackAcrossCleanupDirectory(t *testing.T) {
	directory := t.TempDir()
	cleanupDirectory := filepath.Join(directory, ".gopod-cleanup-test")
	if err := os.Mkdir(cleanupDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	claimedPath := filepath.Join(cleanupDirectory, ownedFileCleanupClaimName)
	originalPath := filepath.Join(directory, "source.part")
	if err := os.WriteFile(claimedPath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := restoreNoReplaceLinux(
		claimedPath,
		originalPath,
		linuxRestoreOperations{
			renameNoReplace: func(_, _ string) error {
				return unix.EOPNOTSUPP
			},
			link:   os.Link,
			remove: os.Remove,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	assertPathMissing(t, claimedPath)
	assertFileContent(t, originalPath, "replacement")
}

func TestRenameNoReplaceLinuxFallsBackToHardLink(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("completed audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	if err := renameNoReplaceLinux(source, destination, operations); err != nil {
		t.Fatal(err)
	}

	assertPathMissing(t, source)
	assertFileContent(t, destination, "completed audio")
}

func TestRenameNoReplaceLinuxCleanupDirectoryFailureKeepsDestination(
	t *testing.T,
) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("completed audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	injectedErr := errors.New("injected cleanup directory removal failure")
	operations.remove = func(path string) error {
		info, err := os.Lstat(path)
		if err == nil &&
			info.IsDir() &&
			strings.HasPrefix(
				filepath.Base(path),
				reservedWorkFilePrefix+"cleanup-",
			) {
			return injectedErr
		}
		return os.Remove(path)
	}
	var warnings []error
	operations.warn = func(err error) {
		warnings = append(warnings, err)
	}

	if err := renameNoReplaceLinux(source, destination, operations); err != nil {
		t.Fatal(err)
	}

	assertPathMissing(t, source)
	assertFileContent(t, destination, "completed audio")
	if len(warnings) != 1 || !errors.Is(warnings[0], injectedErr) {
		t.Fatalf("got cleanup warnings %v, want injected ancillary failure", warnings)
	}
	cleanupDirectories, err := filepath.Glob(
		filepath.Join(directory, ownedFileCleanupDirectoryPattern),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanupDirectories) != 1 {
		t.Fatalf(
			"got cleanup debris %v, want one reserved private directory",
			cleanupDirectories,
		)
	}
	entries, err := os.ReadDir(cleanupDirectories[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf(
			"private cleanup debris contains non-ancillary files: %v",
			entries,
		)
	}
}

func TestRenameNoReplaceLinuxRemoveLostReplyKeepsDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("completed audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	injectedErr := errors.New("injected remove lost reply")
	removeInjected := false
	operations.remove = func(path string) error {
		if filepath.Base(path) == ownedFileCleanupClaimName && !removeInjected {
			removeInjected = true
			if err := os.Remove(path); err != nil {
				return err
			}
			return injectedErr
		}
		return os.Remove(path)
	}
	var warnings []error
	operations.warn = func(err error) {
		warnings = append(warnings, err)
	}

	if err := renameNoReplaceLinux(source, destination, operations); err != nil {
		t.Fatal(err)
	}
	if !removeInjected {
		t.Fatal("remove lost reply was not injected")
	}
	assertPathMissing(t, source)
	assertFileContent(t, destination, "completed audio")
	if len(warnings) != 1 || !errors.Is(warnings[0], injectedErr) {
		t.Fatalf("got cleanup warnings %v, want injected lost reply", warnings)
	}
	assertNoOwnedFileCleanupDirectories(t, directory)
}

func TestRenameNoReplaceLinuxFallbackPreservesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.ENOSYS
	}
	err := renameNoReplaceLinux(source, destination, operations)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("got error %v, want os.ErrExist", err)
	}

	assertFileContent(t, source, "source")
	assertFileContent(t, destination, "existing")
}

func TestRenameNoReplaceLinuxDoesNotFallbackForUnrelatedError(t *testing.T) {
	linkCalled := false
	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EACCES
	}
	operations.link = func(_, _ string) error {
		linkCalled = true
		return nil
	}

	err := renameNoReplaceLinux("/tmp/source", "/tmp/destination", operations)
	if !errors.Is(err, unix.EACCES) {
		t.Fatalf("got error %v, want EACCES", err)
	}
	if linkCalled {
		t.Fatal("hard-link fallback ran for an unrelated rename error")
	}
}

func TestRenameNoReplaceLinuxReportsUnsupportedFallback(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EINVAL
	}
	operations.link = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	err := renameNoReplaceLinux(source, destination, operations)
	if err == nil {
		t.Fatal("expected unsupported publication error")
	}
	if !strings.Contains(err.Error(), "supports neither") {
		t.Fatalf("error %q is not actionable", err)
	}

	assertFileContent(t, source, "source")
	assertPathMissing(t, destination)
}

func TestRenameNoReplaceLinuxRollsBackWhenSourceRemovalFails(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	removeFailed := false
	operations.remove = func(path string) error {
		if filepath.Base(path) == ownedFileCleanupClaimName && !removeFailed {
			removeFailed = true
			return unix.EIO
		}
		return os.Remove(path)
	}
	err := renameNoReplaceLinux(source, destination, operations)
	if !errors.Is(err, unix.EIO) {
		t.Fatalf("got error %v, want source removal failure", err)
	}
	if !strings.Contains(err.Error(), "destination rollback succeeded") {
		t.Fatalf("error %q does not report rollback", err)
	}

	assertFileContent(t, source, "source")
	assertPathMissing(t, destination)
	assertNoOwnedFileCleanupDirectories(t, directory)
}

func TestRenameNoReplaceLinuxMarkerFallbackStaysVisibilityGuarded(
	t *testing.T,
) {
	directory := t.TempDir()
	stem := publicationStem(
		"linux-marker-guard",
		time.Unix(1_700_000_000, 123),
		bytes.Repeat([]byte{0x7a}, publicationTokenSize),
	)
	paths := newPublicationPaths(directory, stem)
	publicationID := strings.Repeat("a", publicationIDTextLength)
	for _, path := range []string{
		paths.ownership,
		paths.visibilityGuard,
		paths.completionStage,
	} {
		if err := os.WriteFile(
			path,
			[]byte(publicationID),
			publicationControlFileMode,
		); err != nil {
			t.Fatal(err)
		}
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	injectedErr := errors.New("injected marker source cleanup failure")
	cleanupBlocked := false
	operations.remove = func(path string) error {
		if filepath.Base(path) == ownedFileCleanupClaimName && !cleanupBlocked {
			cleanupBlocked = true
			assertPathMissing(t, paths.completionStage)
			if _, err := os.Lstat(paths.completionMarker); err != nil {
				t.Fatalf("hard-linked completion marker is missing: %v", err)
			}
			if _, err := os.Lstat(paths.visibilityGuard); err != nil {
				t.Fatalf("visibility guard is missing during marker fallback: %v", err)
			}
			committed, err := hasPublicationCompletionMarker(directory, stem)
			if err != nil {
				t.Fatal(err)
			}
			if committed {
				t.Fatal("marker became feed-visible while its source was claimed")
			}
			return injectedErr
		}
		return os.Remove(path)
	}

	err := renameNoReplaceLinux(
		paths.completionStage,
		paths.completionMarker,
		operations,
	)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("got error %v, want marker source cleanup failure", err)
	}
	if !cleanupBlocked {
		t.Fatal("marker source cleanup was not blocked after its atomic claim")
	}
	if _, err := os.Lstat(paths.completionStage); err != nil {
		t.Fatalf("failed fallback did not restore completion stage: %v", err)
	}
	assertPathMissing(t, paths.completionMarker)
	if _, err := os.Lstat(paths.visibilityGuard); err != nil {
		t.Fatalf("failed fallback removed visibility guard: %v", err)
	}
}

func TestPublishVisibilityMarkerLinuxCleanupFailureIsIrreversible(
	t *testing.T,
) {
	directory := t.TempDir()
	stem := publicationStem(
		"linux-visibility-commit",
		time.Unix(1_700_000_001, 123),
		bytes.Repeat([]byte{0x7b}, publicationTokenSize),
	)
	paths := newPublicationPaths(directory, stem)
	publicationID := strings.Repeat("b", publicationIDTextLength)
	for _, path := range []string{
		paths.ownership,
		paths.completionMarker,
		paths.visibilityGuard,
	} {
		if err := os.WriteFile(
			path,
			[]byte(publicationID),
			publicationControlFileMode,
		); err != nil {
			t.Fatal(err)
		}
	}
	guardIdentity, err := os.Lstat(paths.visibilityGuard)
	if err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	injectedErr := errors.New("injected committed guard cleanup failure")
	cleanupFailed := false
	operations.remove = func(path string) error {
		if filepath.Base(path) == ownedFileCleanupClaimName && !cleanupFailed {
			cleanupFailed = true
			assertPathMissing(t, paths.visibilityGuard)
			if _, err := os.Lstat(paths.visibilityMarker); err != nil {
				t.Fatalf("hard-linked visibility marker is missing: %v", err)
			}
			committed, err := hasPublicationCompletionMarker(directory, stem)
			if err != nil {
				t.Fatal(err)
			}
			if !committed {
				t.Fatal("irreversible visibility commit was not feed-eligible")
			}
			return injectedErr
		}
		return os.Remove(path)
	}
	var warnings []error
	operations.warn = func(err error) {
		warnings = append(warnings, err)
	}

	if err := publishVisibilityMarkerLinux(
		paths.visibilityGuard,
		paths.visibilityMarker,
		guardIdentity,
		operations,
	); err != nil {
		t.Fatal(err)
	}
	if !cleanupFailed {
		t.Fatal("committed guard cleanup failure was not injected")
	}
	assertPathMissing(t, paths.visibilityGuard)
	assertFileContent(t, paths.visibilityMarker, publicationID)
	committed, err := hasPublicationCompletionMarker(directory, stem)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("cleanup failure reversed a committed visibility transition")
	}
	if len(warnings) != 1 || !errors.Is(warnings[0], injectedErr) {
		t.Fatalf("got visibility cleanup warnings %v, want injected failure", warnings)
	}
}

func TestPublishVisibilityMarkerLinuxClaimLostReplyIsIrreversible(
	t *testing.T,
) {
	directory := t.TempDir()
	stem := publicationStem(
		"linux-visibility-claim",
		time.Unix(1_700_000_002, 123),
		bytes.Repeat([]byte{0x7c}, publicationTokenSize),
	)
	paths := newPublicationPaths(directory, stem)
	publicationID := strings.Repeat("c", publicationIDTextLength)
	for _, path := range []string{
		paths.ownership,
		paths.completionMarker,
		paths.visibilityGuard,
	} {
		if err := os.WriteFile(
			path,
			[]byte(publicationID),
			publicationControlFileMode,
		); err != nil {
			t.Fatal(err)
		}
	}
	guardIdentity, err := os.Lstat(paths.visibilityGuard)
	if err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	injectedErr := errors.New("injected guard claim lost reply")
	claimInjected := false
	rename := operations.rename
	operations.rename = func(oldPath, newPath string) error {
		if oldPath == paths.visibilityGuard && !claimInjected {
			claimInjected = true
			if err := rename(oldPath, newPath); err != nil {
				return err
			}
			committed, err := hasPublicationCompletionMarker(directory, stem)
			if err != nil {
				t.Fatal(err)
			}
			if !committed {
				t.Fatal("completed guard claim was not feed-eligible")
			}
			return injectedErr
		}
		return rename(oldPath, newPath)
	}
	var warnings []error
	operations.warn = func(err error) {
		warnings = append(warnings, err)
	}

	if err := publishVisibilityMarkerLinux(
		paths.visibilityGuard,
		paths.visibilityMarker,
		guardIdentity,
		operations,
	); err != nil {
		t.Fatal(err)
	}
	if !claimInjected {
		t.Fatal("guard claim lost reply was not injected")
	}
	assertPathMissing(t, paths.visibilityGuard)
	assertFileContent(t, paths.visibilityMarker, publicationID)
	committed, err := hasPublicationCompletionMarker(directory, stem)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("claim lost reply reversed a committed visibility transition")
	}
	if len(warnings) != 1 || !errors.Is(warnings[0], injectedErr) {
		t.Fatalf("got visibility claim warnings %v, want injected failure", warnings)
	}
}

func TestRenameNoReplaceLinuxRollbackPreservesReplacedDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	destination := filepath.Join(directory, "episode.m4a")
	displacedDestination := destination + ".owned"
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	removeFailed := false
	operations.remove = func(path string) error {
		if filepath.Base(path) == ownedFileCleanupClaimName && !removeFailed {
			removeFailed = true
			if err := os.Rename(destination, displacedDestination); err != nil {
				return err
			}
			if err := os.WriteFile(destination, []byte("replacement"), 0o644); err != nil {
				return err
			}
			return unix.EIO
		}
		return os.Remove(path)
	}
	err := renameNoReplaceLinux(source, destination, operations)
	if !errors.Is(err, unix.EIO) {
		t.Fatalf("got error %v, want source removal failure", err)
	}
	if !strings.Contains(err.Error(), "refuse to remove linked destination") {
		t.Fatalf("error %q does not report ownership mismatch", err)
	}

	assertFileContent(t, source, "source")
	assertFileContent(t, destination, "replacement")
	assertFileContent(t, displacedDestination, "source")
	assertNoOwnedFileCleanupDirectories(t, directory)
}

func TestRenameNoReplaceLinuxFallbackRollsBackSourceSwappedBeforeLink(
	t *testing.T,
) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	displacedSource := source + ".owned"
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	swapped := false
	operations.link = func(oldPath, newPath string) error {
		if oldPath == source && newPath == destination && !swapped {
			if err := os.Rename(source, displacedSource); err != nil {
				return err
			}
			if err := os.WriteFile(source, []byte("replacement"), 0o644); err != nil {
				return err
			}
			swapped = true
		}
		return os.Link(oldPath, newPath)
	}

	err := renameNoReplaceLinux(source, destination, operations)
	if err == nil {
		t.Fatal("expected pre-link source swap failure")
	}
	if !strings.Contains(err.Error(), "changed before hard-link publication") ||
		!strings.Contains(
			err.Error(),
			"linked replacement destination rollback succeeded",
		) {
		t.Fatalf("error %q does not report linked replacement rollback", err)
	}
	if !swapped {
		t.Fatal("source swap was not injected before the hard link")
	}

	assertFileContent(t, source, "replacement")
	assertFileContent(t, displacedSource, "source")
	assertPathMissing(t, destination)
	assertNoOwnedFileCleanupDirectories(t, directory)
}

func TestRenameNoReplaceLinuxFallbackPreservesReplacedSourceBeforeClaim(
	t *testing.T,
) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.part")
	displacedSource := source + ".owned"
	destination := filepath.Join(directory, "episode.m4a")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := testLinuxRenameOperations()
	operations.renameNoReplace = func(_, _ string) error {
		return unix.EOPNOTSUPP
	}
	replaced := false
	operations.rename = func(oldPath, newPath string) error {
		if oldPath == source && !replaced {
			if err := os.Rename(source, displacedSource); err != nil {
				return err
			}
			if err := os.WriteFile(source, []byte("replacement"), 0o644); err != nil {
				return err
			}
			replaced = true
		}
		return os.Rename(oldPath, newPath)
	}

	err := renameNoReplaceLinux(source, destination, operations)
	if err == nil {
		t.Fatal("expected replaced source claim failure")
	}
	if !strings.Contains(err.Error(), "refuse to remove hard-link source") ||
		!strings.Contains(err.Error(), "source removal outcome is unknown") {
		t.Fatalf("error %q does not report preserved unknown source outcome", err)
	}
	if !replaced {
		t.Fatal("source replacement was not injected after the hard link")
	}

	assertFileContent(t, source, "replacement")
	assertFileContent(t, displacedSource, "source")
	assertFileContent(t, destination, "source")
	assertNoOwnedFileCleanupDirectories(t, directory)
}

func testLinuxRenameOperations() linuxRenameOperations {
	return linuxRenameOperations{
		renameNoReplace:  renameNoReplaceLinuxSyscall,
		restoreNoReplace: restoreNoReplace,
		rename:           os.Rename,
		link:             os.Link,
		remove:           os.Remove,
		lstat:            os.Lstat,
		sameFile:         os.SameFile,
		warn:             func(error) {},
	}
}
