//go:build linux

package podcast

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

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
	operations.remove = func(path string) error {
		if path == source {
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
	operations.remove = func(path string) error {
		if path == source {
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
	if !strings.Contains(err.Error(), "refuse to remove destination") {
		t.Fatalf("error %q does not report ownership mismatch", err)
	}

	assertFileContent(t, source, "source")
	assertFileContent(t, destination, "replacement")
	assertFileContent(t, displacedDestination, "source")
}

func testLinuxRenameOperations() linuxRenameOperations {
	return linuxRenameOperations{
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
	}
}
