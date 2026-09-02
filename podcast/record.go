package podcast

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Record(ctx context.Context, podcast Podcast) (recordingPath string, err error) {
	extension := strings.TrimPrefix(podcast.Extension, ".")
	if extension == "" || filepath.Base(extension) != extension {
		return "", fmt.Errorf("invalid recording extension %q", podcast.Extension)
	}

	f, err := os.CreateTemp("", "gopod-recording-*."+extension)
	if err != nil {
		return "", fmt.Errorf("create temporary recording: %w", err)
	}
	tmpFile := f.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, removeOwnedFile(tmpFile))
		}
	}()
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temporary recording %q: %w", tmpFile, err)
	}

	var t time.Time
	t = t.Add(time.Duration(podcast.Length) * time.Second)
	tFormat := t.Format("15:04:05")
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-nostdin",
		"-y",
		"-i", podcast.SourceURL,
		"-t", tFormat,
		"-c", "copy",
		"-map_metadata", "0",
		tmpFile,
	)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("run ffmpeg recorder: %w", ctxErr)
		}
		return "", fmt.Errorf("run ffmpeg recorder: %w", err)
	}
	return tmpFile, nil
}

func Combine(
	ctx context.Context,
	podcast Podcast,
	audioFile string,
	metaFile string,
	descriptionFile string,
) (err error) {
	audioStage, audioStageIdentity, err := createClosedTempFile(
		podcast.Directory,
		".gopod-audio-*.part",
	)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(
			err,
			removeOwnedStagingFile(audioStage, audioStageIdentity),
		)
	}()

	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-nostdin",
		"-y",
		"-i", audioFile,
		"-i", metaFile,
		"-map_metadata", "1",
		"-c:a", "aac",
		"-f", "ipod",
		audioStage,
	)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("run ffmpeg combiner: %w", ctxErr)
		}
		return fmt.Errorf("run ffmpeg combiner: %w", err)
	}
	if err := verifyCanonicalPublicationStage(
		"audio",
		audioStage,
		audioStageIdentity,
	); err != nil {
		return err
	}
	if err := os.Chmod(audioStage, 0o644); err != nil {
		return fmt.Errorf("set podcast audio permissions %q: %w", audioStage, err)
	}
	if err := verifyCanonicalPublicationStage(
		"audio",
		audioStage,
		audioStageIdentity,
	); err != nil {
		return err
	}

	descriptionStage := ""
	var descriptionStageIdentity os.FileInfo
	if descriptionFile != "" {
		data, err := os.ReadFile(descriptionFile)
		if err != nil {
			return fmt.Errorf(
				"read podcast description %q: %w",
				descriptionFile,
				err,
			)
		}
		descriptionStage, descriptionStageIdentity, err = createClosedTempFile(
			podcast.Directory,
			".gopod-description-*.part",
		)
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(
				err,
				removeOwnedStagingFile(
					descriptionStage,
					descriptionStageIdentity,
				),
			)
		}()
		if err := os.WriteFile(descriptionStage, data, 0o644); err != nil {
			return fmt.Errorf("write temporary podcast description %q: %w", descriptionStage, err)
		}
		if err := os.Chmod(descriptionStage, 0o644); err != nil {
			return fmt.Errorf(
				"set podcast description permissions %q: %w",
				descriptionStage,
				err,
			)
		}
		if err := verifyCanonicalPublicationStage(
			"description",
			descriptionStage,
			descriptionStageIdentity,
		); err != nil {
			return err
		}
	}

	if _, _, err := publishCompletedRecording(
		podcast,
		audioStage,
		descriptionStage,
		time.Now(),
	); err != nil {
		return err
	}
	return nil
}

func createClosedTempFile(
	directory string,
	pattern string,
) (path string, identity os.FileInfo, err error) {
	f, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", nil, fmt.Errorf(
			"create temporary podcast file in %q: %w",
			directory,
			err,
		)
	}
	path = f.Name()
	defer func() {
		if err != nil {
			var cleanupErr error
			if identity == nil {
				cleanupErr = removeOwnedFile(path)
			} else {
				cleanupErr = removeOwnedStagingFile(path, identity)
			}
			err = errors.Join(err, f.Close(), cleanupErr)
		}
	}()
	identity, err = f.Stat()
	if err != nil {
		return path, identity, fmt.Errorf(
			"inspect temporary podcast file %q: %w",
			path,
			err,
		)
	}
	if closeErr := f.Close(); closeErr != nil {
		err = fmt.Errorf("close temporary podcast file %q: %w", path, closeErr)
		return
	}
	return path, identity, nil
}

func publishCompletedRecording(
	podcast Podcast,
	audioStage string,
	descriptionStage string,
	recordedAt time.Time,
) (audioPath string, descriptionPath string, err error) {
	operations := newPublicationOperations(renameNoReplace)
	operations.publishVisibility = publishVisibilityMarker
	return publishCompletedRecordingWithOperations(
		podcast,
		audioStage,
		descriptionStage,
		recordedAt,
		rand.Reader,
		operations,
	)
}

type renameFileFunc func(oldPath, newPath string) error

type publicationOperations struct {
	renameFile        renameFileFunc
	publishVisibility func(
		guardPath string,
		visibilityMarkerPath string,
		expectedIdentity os.FileInfo,
	) error
	syncPublishedFile func(kind, path string, expectedIdentity os.FileInfo) error
	syncDirectory     func(path string) error
}

func newPublicationOperations(renameFile renameFileFunc) publicationOperations {
	return publicationOperations{
		renameFile: renameFile,
		publishVisibility: func(
			guardPath string,
			visibilityMarkerPath string,
			_ os.FileInfo,
		) error {
			return renameFile(guardPath, visibilityMarkerPath)
		},
		syncPublishedFile: syncPublishedFile,
		syncDirectory:     syncPublicationDirectory,
	}
}

const (
	publicationMaxAttempts           = 100
	publicationTokenSize             = 16
	ownedFileCleanupDirectoryPattern = reservedWorkFilePrefix + "cleanup-*"
	ownedFileCleanupClaimName        = "claimed"
)

var errPublicationCollision = errors.New("publication destination collision")

func publishCompletedRecordingWith(
	podcast Podcast,
	audioStage string,
	descriptionStage string,
	recordedAt time.Time,
	random io.Reader,
	renameFile renameFileFunc,
) (audioPath string, descriptionPath string, err error) {
	return publishCompletedRecordingWithOperations(
		podcast,
		audioStage,
		descriptionStage,
		recordedAt,
		random,
		newPublicationOperations(renameFile),
	)
}

func publishCompletedRecordingWithOperations(
	podcast Podcast,
	audioStage string,
	descriptionStage string,
	recordedAt time.Time,
	random io.Reader,
	operations publicationOperations,
) (audioPath string, descriptionPath string, err error) {
	if err := ValidateShortTitle(podcast.ShortTitle); err != nil {
		return "", "", fmt.Errorf(
			"invalid podcast short title for output filename: %w",
			err,
		)
	}

	audioStageIdentity, err := inspectCanonicalPublicationStage(
		"audio",
		audioStage,
	)
	if err != nil {
		return "", "", err
	}
	var descriptionStageIdentity os.FileInfo
	if descriptionStage != "" {
		descriptionStageIdentity, err = inspectCanonicalPublicationStage(
			"description",
			descriptionStage,
		)
		if err != nil {
			return "", "", err
		}
	}

	token := make([]byte, publicationTokenSize)
	attemptedStems := make(map[string]struct{}, publicationMaxAttempts)
	for attempt := 0; attempt < publicationMaxAttempts; attempt++ {
		if _, err := io.ReadFull(random, token); err != nil {
			return "", "", fmt.Errorf("generate unique podcast output name: %w", err)
		}
		stem := publicationStem(podcast.ShortTitle, recordedAt, token)
		if _, attempted := attemptedStems[stem]; attempted {
			continue
		}
		attemptedStems[stem] = struct{}{}

		paths := newPublicationPaths(podcast.Directory, stem)
		unused, err := publicationPathsUnused(paths.all()...)
		if err != nil {
			return "", "", err
		}
		if !unused {
			continue
		}

		if err := publishOwnedRecording(
			audioStage,
			audioStageIdentity,
			descriptionStage,
			descriptionStageIdentity,
			paths,
			operations,
		); err != nil {
			if errors.Is(err, errPublicationCollision) {
				continue
			}
			return "", "", err
		}
		descriptionPath = ""
		if descriptionStage != "" {
			descriptionPath = paths.description
		}
		return paths.audio, descriptionPath, nil
	}

	return "", "", fmt.Errorf(
		"publish podcast %q: exhausted %d unique output names",
		podcast.ShortTitle,
		publicationMaxAttempts,
	)
}

type publicationPaths struct {
	stem             string
	audio            string
	description      string
	ownership        string
	completionMarker string
	completionStage  string
	visibilityGuard  string
	visibilityMarker string
}

func newPublicationPaths(directory, stem string) publicationPaths {
	return publicationPaths{
		stem:             stem,
		audio:            filepath.Join(directory, stem+".m4a"),
		description:      filepath.Join(directory, stem+".txt"),
		ownership:        publicationOwnershipPath(directory, stem),
		completionMarker: publicationCompletionMarkerPath(directory, stem),
		completionStage:  publicationCompletionStagePath(directory, stem),
		visibilityGuard:  publicationVisibilityGuardPath(directory, stem),
		visibilityMarker: publicationVisibilityMarkerPath(directory, stem),
	}
}

func (paths publicationPaths) all() []string {
	return []string{
		paths.audio,
		paths.description,
		paths.ownership,
		paths.completionMarker,
		paths.completionStage,
		paths.visibilityGuard,
		paths.visibilityMarker,
	}
}

func publicationStem(shortTitle string, recordedAt time.Time, token []byte) string {
	return fmt.Sprintf(
		"%s-%d-%s-%s",
		shortTitle,
		recordedAt.UnixNano(),
		publicationProtocol,
		hex.EncodeToString(token),
	)
}

func publicationPathsUnused(paths ...string) (bool, error) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("check podcast output path %q: %w", path, err)
		}
	}
	return true, nil
}

type publicationOwnership struct {
	path     string
	id       string
	identity os.FileInfo
}

func publishOwnedRecording(
	audioStage string,
	audioStageIdentity os.FileInfo,
	descriptionStage string,
	descriptionStageIdentity os.FileInfo,
	paths publicationPaths,
	operations publicationOperations,
) (err error) {
	ownership, err := createPublicationOwnership(paths.ownership)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return publicationCollisionError("ownership", paths.ownership)
		}
		return err
	}
	preserveOwnership := false
	defer func() {
		if preserveOwnership {
			return
		}
		cleanupErr := removeOwnedOwnershipFile(
			ownership.path,
			ownership.identity,
		)
		if cleanupErr == nil {
			return
		}
		if errors.Is(err, errPublicationCollision) {
			err = errors.Join(
				fmt.Errorf(
					"cannot retry after podcast publication collision: %v",
					err,
				),
				cleanupErr,
			)
			return
		}
		err = errors.Join(err, cleanupErr)
	}()

	unused, err := publicationPathsUnused(
		paths.audio,
		paths.description,
		paths.completionMarker,
		paths.completionStage,
		paths.visibilityGuard,
		paths.visibilityMarker,
	)
	if err != nil {
		return err
	}
	if !unused {
		return publicationCollisionError(
			"post-ownership path",
			paths.stem,
		)
	}
	if err := verifyPublicationOwnership(ownership); err != nil {
		return err
	}

	err = publishStagedRecording(
		audioStage,
		audioStageIdentity,
		descriptionStage,
		descriptionStageIdentity,
		paths,
		ownership,
		operations,
		&preserveOwnership,
	)
	return err
}

func createPublicationOwnership(path string) (ownership publicationOwnership, err error) {
	publicationIDBytes := make([]byte, publicationIDSize)
	if _, err := rand.Read(publicationIDBytes); err != nil {
		return publicationOwnership{}, fmt.Errorf(
			"generate podcast publication ID: %w",
			err,
		)
	}
	ownership = publicationOwnership{
		path: path,
		id:   hex.EncodeToString(publicationIDBytes),
	}

	f, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		publicationControlFileMode,
	)
	if err != nil {
		return publicationOwnership{}, fmt.Errorf(
			"acquire podcast publication ownership %q: %w",
			path,
			err,
		)
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := f.Close(); closeErr != nil {
				err = errors.Join(
					err,
					fmt.Errorf(
						"close podcast publication ownership %q: %w",
						path,
						closeErr,
					),
				)
			}
		}
		if err != nil && ownership.identity != nil {
			err = errors.Join(
				err,
				removeOwnedOwnershipFile(path, ownership.identity),
			)
		}
	}()

	ownership.identity, err = f.Stat()
	if err != nil {
		return ownership, fmt.Errorf(
			"inspect podcast publication ownership %q: %w",
			path,
			err,
		)
	}
	if _, err := io.WriteString(f, ownership.id); err != nil {
		return ownership, fmt.Errorf(
			"write podcast publication ownership %q: %w",
			path,
			err,
		)
	}
	if err := f.Sync(); err != nil {
		return ownership, fmt.Errorf(
			"sync podcast publication ownership %q: %w",
			path,
			err,
		)
	}
	closeErr := f.Close()
	closed = true
	if closeErr != nil {
		return ownership, fmt.Errorf(
			"close podcast publication ownership %q: %w",
			path,
			closeErr,
		)
	}
	if err := verifyPublicationOwnership(ownership); err != nil {
		return ownership, err
	}
	return ownership, nil
}

func publishStagedRecording(
	audioStage string,
	audioStageIdentity os.FileInfo,
	descriptionStage string,
	descriptionStageIdentity os.FileInfo,
	paths publicationPaths,
	ownership publicationOwnership,
	operations publicationOperations,
	preserveOwnership *bool,
) error {
	descriptionPublicationStage := ""
	var descriptionPublicationIdentity os.FileInfo
	if descriptionStage != "" {
		var err error
		descriptionPublicationStage, descriptionPublicationIdentity, err =
			createDescriptionPublicationStage(
				descriptionStage,
				descriptionStageIdentity,
			)
		if err != nil {
			return err
		}
	}

	if descriptionPublicationStage != "" {
		published, publishErr := publishStagedFile(
			"description",
			descriptionPublicationStage,
			descriptionPublicationIdentity,
			paths.description,
			operations.renameFile,
		)
		if published {
			*preserveOwnership = true
		}
		cleanupErr := removeDescriptionPublicationStage(
			descriptionPublicationStage,
			descriptionPublicationIdentity,
		)
		if publishErr != nil {
			var collision *publicationPathCollisionError
			if !errors.As(publishErr, &collision) {
				*preserveOwnership = true
				return errors.Join(publishErr, cleanupErr)
			}
			return retryPublicationCollision(
				collision,
				cleanupErr,
				audioStage,
				audioStageIdentity,
				descriptionStage,
				descriptionStageIdentity,
			)
		}
		if cleanupErr != nil {
			*preserveOwnership = true
			return cleanupErr
		}
	}

	audioPublished, publishErr := publishStagedFile(
		"audio",
		audioStage,
		audioStageIdentity,
		paths.audio,
		operations.renameFile,
	)
	if audioPublished {
		*preserveOwnership = true
	}
	if publishErr != nil {
		var collision *publicationPathCollisionError
		if !errors.As(publishErr, &collision) {
			*preserveOwnership = true
			return publishErr
		}
		return retryPublicationCollision(
			collision,
			nil,
			audioStage,
			audioStageIdentity,
			descriptionStage,
			descriptionStageIdentity,
		)
	}

	return commitPublicationCompletion(
		paths,
		ownership,
		audioStageIdentity,
		descriptionPublicationIdentity,
		descriptionStage != "",
		operations,
	)
}

type publicationPathCollisionError struct {
	kind string
	path string
}

func (err *publicationPathCollisionError) Error() string {
	return fmt.Sprintf(
		"%s destination %q appeared during publication",
		err.kind,
		err.path,
	)
}

func publishStagedFile(
	kind string,
	stagePath string,
	stageIdentity os.FileInfo,
	destinationPath string,
	renameFile renameFileFunc,
) (published bool, err error) {
	if err := renameFile(stagePath, destinationPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, &publicationPathCollisionError{
				kind: kind,
				path: destinationPath,
			}
		}
		return false, fmt.Errorf(
			"publish podcast %s %q: %w",
			kind,
			destinationPath,
			err,
		)
	}
	if err := verifyPublishedFile(kind, destinationPath, stageIdentity); err != nil {
		return true, err
	}
	return true, nil
}

func retryPublicationCollision(
	collision *publicationPathCollisionError,
	cleanupErr error,
	audioStage string,
	audioStageIdentity os.FileInfo,
	descriptionStage string,
	descriptionStageIdentity os.FileInfo,
) error {
	reusableErr := verifyCanonicalPublicationStages(
		audioStage,
		audioStageIdentity,
		descriptionStage,
		descriptionStageIdentity,
	)
	if cleanupErr != nil || reusableErr != nil {
		return errors.Join(
			fmt.Errorf(
				"cannot retry podcast %s destination collision at %q",
				collision.kind,
				collision.path,
			),
			cleanupErr,
			reusableErr,
		)
	}
	return publicationCollisionError(collision.kind, collision.path)
}

func publicationCollisionError(kind, path string) error {
	return fmt.Errorf(
		"%w: %s destination %q appeared during publication",
		errPublicationCollision,
		kind,
		path,
	)
}

func verifyPublicationReadyForCommit(
	paths publicationPaths,
	ownership publicationOwnership,
	audioIdentity os.FileInfo,
	descriptionIdentity os.FileInfo,
	hasDescription bool,
) error {
	ownershipErr := verifyPublicationOwnership(ownership)
	audioErr := verifyPublishedFile("audio", paths.audio, audioIdentity)
	var descriptionErr error
	if hasDescription {
		descriptionErr = verifyPublishedFile(
			"description",
			paths.description,
			descriptionIdentity,
		)
	} else if _, err := os.Lstat(paths.description); err == nil {
		descriptionErr = fmt.Errorf(
			"unexpected podcast description %q appeared before commit",
			paths.description,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		descriptionErr = fmt.Errorf(
			"inspect absent podcast description %q before commit: %w",
			paths.description,
			err,
		)
	}
	return errors.Join(ownershipErr, audioErr, descriptionErr)
}

func verifyPublishedFile(
	kind string,
	path string,
	expectedIdentity os.FileInfo,
) error {
	currentIdentity, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf(
			"inspect published podcast %s %q: %w",
			kind,
			path,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, currentIdentity) {
		return fmt.Errorf(
			"published podcast %s %q ownership changed",
			kind,
			path,
		)
	}
	return nil
}

func verifyPublicationOwnership(ownership publicationOwnership) error {
	currentIdentity, err := os.Lstat(ownership.path)
	if err != nil {
		return fmt.Errorf(
			"inspect podcast publication ownership %q: %w",
			ownership.path,
			err,
		)
	}
	if !os.SameFile(ownership.identity, currentIdentity) {
		return fmt.Errorf(
			"podcast publication ownership %q ownership changed",
			ownership.path,
		)
	}

	publicationID, valid, err := readPublicationIDFile(
		ownership.path,
		"ownership",
	)
	if err != nil {
		return err
	}
	if !valid || publicationID != ownership.id {
		return fmt.Errorf(
			"podcast publication ownership %q contains an invalid or unexpected publication ID",
			ownership.path,
		)
	}

	currentIdentity, err = os.Lstat(ownership.path)
	if err != nil {
		return fmt.Errorf(
			"reinspect podcast publication ownership %q: %w",
			ownership.path,
			err,
		)
	}
	if !os.SameFile(ownership.identity, currentIdentity) {
		return fmt.Errorf(
			"podcast publication ownership %q ownership changed",
			ownership.path,
		)
	}
	return nil
}

type publicationControl struct {
	path     string
	id       string
	identity os.FileInfo
}

func createPublicationControl(
	path string,
	publicationID string,
	label string,
) (control publicationControl, err error) {
	control = publicationControl{
		path: path,
		id:   publicationID,
	}
	f, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		publicationControlFileMode,
	)
	if err != nil {
		return control, fmt.Errorf(
			"create podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := f.Close(); closeErr != nil {
				err = errors.Join(
					err,
					fmt.Errorf(
						"close podcast publication %s %q: %w",
						label,
						path,
						closeErr,
					),
				)
			}
		}
	}()

	control.identity, err = f.Stat()
	if err != nil {
		return control, fmt.Errorf(
			"inspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if _, err := io.WriteString(f, publicationID); err != nil {
		return control, fmt.Errorf(
			"write podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if err := f.Sync(); err != nil {
		return control, fmt.Errorf(
			"sync podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	closeErr := f.Close()
	closed = true
	if closeErr != nil {
		return control, fmt.Errorf(
			"close podcast publication %s %q: %w",
			label,
			path,
			closeErr,
		)
	}
	if err := verifyPublicationControlFile(
		path,
		control.identity,
		publicationID,
		label,
	); err != nil {
		return control, err
	}
	return control, nil
}

type publicationCommitState string

const (
	publicationStateFilesPublished publicationCommitState = "files-published"
	publicationStateFilesSynced    publicationCommitState = "files-synced"
	publicationStateGuarded        publicationCommitState = "visibility-guarded"
	publicationStatePrepared       publicationCommitState = "marker-prepared"
	publicationStateMarkerDurable  publicationCommitState = "marker-durable"
	publicationStateVisible        publicationCommitState = "visible"
)

func commitPublicationCompletion(
	paths publicationPaths,
	ownership publicationOwnership,
	audioIdentity os.FileInfo,
	descriptionIdentity os.FileInfo,
	hasDescription bool,
	operations publicationOperations,
) error {
	state := publicationStateFilesPublished
	if err := operations.syncPublishedFile(
		"audio",
		paths.audio,
		audioIdentity,
	); err != nil {
		return fmt.Errorf(
			"sync podcast publication audio while %s: %w",
			state,
			err,
		)
	}
	if hasDescription {
		if err := operations.syncPublishedFile(
			"description",
			paths.description,
			descriptionIdentity,
		); err != nil {
			return fmt.Errorf(
				"sync podcast publication description while %s: %w",
				state,
				err,
			)
		}
	}
	state = publicationStateFilesSynced

	guard, err := createPublicationControl(
		paths.visibilityGuard,
		ownership.id,
		"visibility guard",
	)
	if err != nil {
		return fmt.Errorf(
			"guard podcast publication while %s: %w",
			state,
			err,
		)
	}
	state = publicationStateGuarded

	completionStage, err := createPublicationControl(
		paths.completionStage,
		ownership.id,
		"completion stage",
	)
	if err != nil {
		return fmt.Errorf(
			"prepare podcast publication marker while %s: %w",
			state,
			err,
		)
	}
	state = publicationStatePrepared

	preparedErr := errors.Join(
		verifyPublicationReadyForCommit(
			paths,
			ownership,
			audioIdentity,
			descriptionIdentity,
			hasDescription,
		),
		verifyPublicationControlFile(
			guard.path,
			guard.identity,
			guard.id,
			"visibility guard",
		),
		verifyPublicationControlFile(
			completionStage.path,
			completionStage.identity,
			completionStage.id,
			"completion stage",
		),
	)
	if preparedErr != nil {
		return fmt.Errorf(
			"verify podcast publication while %s: %w",
			state,
			preparedErr,
		)
	}
	if _, err := os.Lstat(paths.completionMarker); err == nil {
		return fmt.Errorf(
			"podcast publication completion marker %q appeared while %s",
			paths.completionMarker,
			state,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"inspect podcast publication completion marker %q while %s: %w",
			paths.completionMarker,
			state,
			err,
		)
	}
	directory := filepath.Dir(paths.audio)
	if err := operations.syncDirectory(directory); err != nil {
		return fmt.Errorf(
			"sync podcast publication directory %q before marker publication while %s: %w",
			directory,
			state,
			err,
		)
	}

	if err := operations.renameFile(
		completionStage.path,
		paths.completionMarker,
	); err != nil {
		return fmt.Errorf(
			"commit podcast publication marker %q while %s: %w",
			paths.completionMarker,
			state,
			err,
		)
	}
	if _, err := os.Lstat(completionStage.path); err == nil {
		return fmt.Errorf(
			"podcast publication completion stage %q remains after marker publication",
			completionStage.path,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"inspect podcast publication completion stage %q after marker publication: %w",
			completionStage.path,
			err,
		)
	}

	committedErr := errors.Join(
		verifyPublicationControlFile(
			paths.completionMarker,
			completionStage.identity,
			ownership.id,
			"completion marker",
		),
		verifyPublicationReadyForCommit(
			paths,
			ownership,
			audioIdentity,
			descriptionIdentity,
			hasDescription,
		),
		verifyPublicationControlFile(
			guard.path,
			guard.identity,
			guard.id,
			"visibility guard",
		),
	)
	if committedErr != nil {
		return fmt.Errorf(
			"verify guarded podcast publication after marker publication: %w",
			committedErr,
		)
	}
	if err := operations.syncDirectory(directory); err != nil {
		return fmt.Errorf(
			"sync podcast publication directory %q after marker publication: %w",
			directory,
			err,
		)
	}
	state = publicationStateMarkerDurable

	if err := commitPublicationVisibility(
		paths,
		guard,
		ownership,
		operations.publishVisibility,
	); err != nil {
		return fmt.Errorf(
			"commit podcast publication visibility while %s: %w",
			state,
			err,
		)
	}
	state = publicationStateVisible

	if err := operations.syncDirectory(directory); err != nil {
		return fmt.Errorf(
			"post-commit durability error: sync podcast publication directory %q after visibility marker publication while %s: %w",
			directory,
			state,
			err,
		)
	}
	return nil
}

func verifyPublicationControlFile(
	path string,
	expectedIdentity os.FileInfo,
	expectedID string,
	label string,
) error {
	currentIdentity, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf(
			"inspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, currentIdentity) {
		return fmt.Errorf(
			"podcast publication %s %q ownership changed",
			label,
			path,
		)
	}
	publicationID, valid, err := readPublicationIDFile(path, label)
	if err != nil {
		return err
	}
	if !valid || publicationID != expectedID {
		return fmt.Errorf(
			"podcast publication %s %q contains an invalid or unexpected publication ID",
			label,
			path,
		)
	}
	currentIdentity, err = os.Lstat(path)
	if err != nil {
		return fmt.Errorf(
			"reinspect podcast publication %s %q: %w",
			label,
			path,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, currentIdentity) {
		return fmt.Errorf(
			"podcast publication %s %q ownership changed",
			label,
			path,
		)
	}
	return nil
}

func commitPublicationVisibility(
	paths publicationPaths,
	guard publicationControl,
	ownership publicationOwnership,
	publishVisibility func(
		guardPath string,
		visibilityMarkerPath string,
		expectedIdentity os.FileInfo,
	) error,
) error {
	if err := verifyPublicationOwnership(ownership); err != nil {
		return err
	}
	if err := verifyPublicationControlFile(
		guard.path,
		guard.identity,
		guard.id,
		"visibility guard",
	); err != nil {
		return err
	}
	if _, err := os.Lstat(paths.visibilityMarker); err == nil {
		return fmt.Errorf(
			"podcast publication visibility marker %q appeared before commit",
			paths.visibilityMarker,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"inspect podcast publication visibility marker %q before commit: %w",
			paths.visibilityMarker,
			err,
		)
	}

	if err := publishVisibility(
		guard.path,
		paths.visibilityMarker,
		guard.identity,
	); err != nil {
		moved, inspectErr := publicationVisibilityMoveCompleted(
			guard,
			paths.visibilityMarker,
		)
		if moved {
			return nil
		}
		guardErr := ensurePublicationVisibilityGuard(guard)
		return errors.Join(
			fmt.Errorf(
				"publish podcast visibility marker %q: %w",
				paths.visibilityMarker,
				err,
			),
			inspectErr,
			guardErr,
		)
	}
	if _, err := os.Lstat(guard.path); err == nil {
		return fmt.Errorf(
			"podcast publication visibility guard %q remains after visibility commit",
			guard.path,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"inspect podcast publication visibility guard %q after visibility commit: %w",
			guard.path,
			err,
		)
	}
	return errors.Join(
		verifyPublicationOwnership(ownership),
		verifyPublicationControlFile(
			paths.visibilityMarker,
			guard.identity,
			guard.id,
			"visibility marker",
		),
	)
}

func publicationVisibilityMoveCompleted(
	guard publicationControl,
	visibilityMarkerPath string,
) (bool, error) {
	if _, err := os.Lstat(guard.path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf(
			"inspect podcast publication visibility guard %q after failed move: %w",
			guard.path,
			err,
		)
	}
	if err := verifyPublicationControlFile(
		visibilityMarkerPath,
		guard.identity,
		guard.id,
		"visibility marker",
	); err != nil {
		return false, fmt.Errorf(
			"verify podcast publication visibility marker after failed move: %w",
			err,
		)
	}
	return true, nil
}

func ensurePublicationVisibilityGuard(guard publicationControl) error {
	if _, err := os.Lstat(guard.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"inspect podcast publication visibility guard %q after failed commit: %w",
			guard.path,
			err,
		)
	}
	if _, err := createPublicationControl(
		guard.path,
		guard.id,
		"visibility guard",
	); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf(
			"restore podcast publication visibility guard %q after failed commit: %w",
			guard.path,
			err,
		)
	}
	return nil
}

func syncPublishedFile(
	kind string,
	path string,
	expectedIdentity os.FileInfo,
) (err error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf(
			"open published podcast %s %q for sync: %w",
			kind,
			path,
			err,
		)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf(
					"close published podcast %s %q after sync: %w",
					kind,
					path,
					closeErr,
				),
			)
		}
	}()

	if err := verifyOpenedPublishedFile(
		kind,
		path,
		file,
		expectedIdentity,
		"before sync",
	); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf(
			"sync published podcast %s %q: %w",
			kind,
			path,
			err,
		)
	}
	return verifyOpenedPublishedFile(
		kind,
		path,
		file,
		expectedIdentity,
		"after sync",
	)
}

func verifyOpenedPublishedFile(
	kind string,
	path string,
	file *os.File,
	expectedIdentity os.FileInfo,
	phase string,
) error {
	openedIdentity, err := file.Stat()
	if err != nil {
		return fmt.Errorf(
			"inspect open published podcast %s %q %s: %w",
			kind,
			path,
			phase,
			err,
		)
	}
	pathIdentity, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf(
			"inspect published podcast %s %q %s: %w",
			kind,
			path,
			phase,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, openedIdentity) ||
		!os.SameFile(expectedIdentity, pathIdentity) {
		return fmt.Errorf(
			"published podcast %s %q ownership changed %s",
			kind,
			path,
			phase,
		)
	}
	return nil
}

func inspectCanonicalPublicationStage(
	kind string,
	path string,
) (os.FileInfo, error) {
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf(
			"inspect canonical podcast %s staging file %q: %w",
			kind,
			path,
			err,
		)
	}
	return identity, nil
}

func verifyCanonicalPublicationStages(
	audioStage string,
	audioStageIdentity os.FileInfo,
	descriptionStage string,
	descriptionStageIdentity os.FileInfo,
) error {
	audioErr := verifyCanonicalPublicationStage(
		"audio",
		audioStage,
		audioStageIdentity,
	)
	var descriptionErr error
	if descriptionStage != "" {
		descriptionErr = verifyCanonicalPublicationStage(
			"description",
			descriptionStage,
			descriptionStageIdentity,
		)
	}
	return errors.Join(audioErr, descriptionErr)
}

func verifyCanonicalPublicationStage(
	kind string,
	path string,
	expectedIdentity os.FileInfo,
) error {
	currentIdentity, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf(
			"inspect canonical podcast %s staging file %q: %w",
			kind,
			path,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, currentIdentity) {
		return fmt.Errorf(
			"canonical podcast %s staging file %q ownership changed",
			kind,
			path,
		)
	}
	return nil
}

func createDescriptionPublicationStage(
	descriptionStage string,
	expectedIdentity os.FileInfo,
) (stagePath string, stageIdentity os.FileInfo, err error) {
	source, err := os.Open(descriptionStage)
	if err != nil {
		return "", nil, fmt.Errorf(
			"open canonical podcast description staging file %q: %w",
			descriptionStage,
			err,
		)
	}
	sourceClosed := false
	var destination *os.File
	destinationClosed := false
	ownedPath := ""
	var ownedIdentity os.FileInfo
	defer func() {
		if destination != nil && !destinationClosed {
			if closeErr := destination.Close(); closeErr != nil {
				err = errors.Join(
					err,
					fmt.Errorf(
						"close disposable podcast description staging file %q: %w",
						ownedPath,
						closeErr,
					),
				)
			}
		}
		if !sourceClosed {
			if closeErr := source.Close(); closeErr != nil {
				err = errors.Join(
					err,
					fmt.Errorf(
						"close canonical podcast description staging file %q: %w",
						descriptionStage,
						closeErr,
					),
				)
			}
		}
		if err != nil && ownedPath != "" {
			var cleanupErr error
			if ownedIdentity == nil {
				cleanupErr = removeOwnedFile(ownedPath)
			} else {
				cleanupErr = removeOwnedStagingFile(
					ownedPath,
					ownedIdentity,
				)
			}
			err = errors.Join(err, cleanupErr)
		}
	}()

	openedIdentity, err := source.Stat()
	if err != nil {
		return "", nil, fmt.Errorf(
			"inspect open canonical podcast description staging file %q: %w",
			descriptionStage,
			err,
		)
	}
	if !os.SameFile(expectedIdentity, openedIdentity) {
		return "", nil, fmt.Errorf(
			"canonical podcast description staging file %q ownership changed before copy",
			descriptionStage,
		)
	}

	destination, err = os.CreateTemp(
		filepath.Dir(descriptionStage),
		".gopod-description-publish-*.part",
	)
	if err != nil {
		return "", nil, fmt.Errorf(
			"create disposable podcast description staging file: %w",
			err,
		)
	}
	ownedPath = destination.Name()
	ownedIdentity, err = destination.Stat()
	if err != nil {
		return "", nil, fmt.Errorf(
			"inspect disposable podcast description staging file %q: %w",
			ownedPath,
			err,
		)
	}
	if _, err := io.Copy(destination, source); err != nil {
		return "", nil, fmt.Errorf(
			"copy canonical podcast description staging file %q to %q: %w",
			descriptionStage,
			ownedPath,
			err,
		)
	}
	if err := destination.Chmod(openedIdentity.Mode().Perm()); err != nil {
		return "", nil, fmt.Errorf(
			"set disposable podcast description permissions %q: %w",
			ownedPath,
			err,
		)
	}
	closeErr := destination.Close()
	destinationClosed = true
	if closeErr != nil {
		return "", nil, fmt.Errorf(
			"close disposable podcast description staging file %q: %w",
			ownedPath,
			closeErr,
		)
	}
	closeErr = source.Close()
	sourceClosed = true
	if closeErr != nil {
		return "", nil, fmt.Errorf(
			"close canonical podcast description staging file %q: %w",
			descriptionStage,
			closeErr,
		)
	}
	if err := verifyCanonicalPublicationStage(
		"description",
		descriptionStage,
		expectedIdentity,
	); err != nil {
		return "", nil, err
	}

	return ownedPath, ownedIdentity, nil
}

func removeDescriptionPublicationStage(
	path string,
	identity os.FileInfo,
) error {
	if path == "" {
		return nil
	}
	if err := removeOwnedStagingFile(path, identity); err != nil {
		return fmt.Errorf(
			"clean up disposable podcast description staging file %q: %w",
			path,
			err,
		)
	}
	return nil
}

func removeOwnedStagingFile(path string, identity os.FileInfo) error {
	return removeOwnedFileWithIdentity(path, identity, "staging file")
}

func removeOwnedOwnershipFile(path string, identity os.FileInfo) error {
	return removeOwnedFileWithIdentity(path, identity, "publication ownership")
}

type ownedFileCleanupOperations struct {
	rename          func(oldPath, newPath string) error
	renameNoReplace func(oldPath, newPath string) error
	remove          func(path string) error
	lstat           func(path string) (os.FileInfo, error)
	sameFile        func(first, second os.FileInfo) bool
}

type ownedFileCleanupResult struct {
	fileRemoved  bool
	fileRetained bool
	err          error
	ancillaryErr error
}

func (result ownedFileCleanupResult) combinedError() error {
	return errors.Join(result.err, result.ancillaryErr)
}

func removeOwnedFileWithIdentity(
	path string,
	identity os.FileInfo,
	label string,
) error {
	return removeOwnedFileWithIdentityUsing(
		path,
		identity,
		label,
		ownedFileCleanupOperations{
			rename:          os.Rename,
			renameNoReplace: restoreNoReplace,
			remove:          os.Remove,
			lstat:           os.Lstat,
			sameFile:        os.SameFile,
		},
	)
}

func removeOwnedFileWithIdentityUsing(
	path string,
	identity os.FileInfo,
	label string,
	operations ownedFileCleanupOperations,
) error {
	return removeOwnedFileWithIdentityUsingResult(
		path,
		identity,
		label,
		operations,
	).combinedError()
}

func removeOwnedFileWithIdentityUsingResult(
	path string,
	identity os.FileInfo,
	label string,
	operations ownedFileCleanupOperations,
) ownedFileCleanupResult {
	if path == "" {
		return ownedFileCleanupResult{fileRemoved: true}
	}
	if identity == nil {
		return ownedFileCleanupResult{
			err: fmt.Errorf(
				"refuse to remove %s %q without an expected file identity",
				label,
				path,
			),
		}
	}
	if _, err := operations.lstat(path); errors.Is(err, os.ErrNotExist) {
		return ownedFileCleanupResult{fileRemoved: true}
	} else if err != nil {
		return ownedFileCleanupResult{
			err: fmt.Errorf(
				"inspect owned %s %q before atomic cleanup claim: %w",
				label,
				path,
				err,
			),
		}
	}

	cleanupDirectory, err := os.MkdirTemp(
		filepath.Dir(path),
		ownedFileCleanupDirectoryPattern,
	)
	if err != nil {
		return classifyOwnedFileCleanupFailure(
			path,
			identity,
			fmt.Errorf(
				"create private cleanup directory for %s %q: %w",
				label,
				path,
				err,
			),
			nil,
			operations,
		)
	}
	claimedPath := filepath.Join(
		cleanupDirectory,
		ownedFileCleanupClaimName,
	)
	if err := operations.rename(path, claimedPath); err != nil {
		directoryErr := removeEmptyOwnedFileCleanupDirectory(
			cleanupDirectory,
			operations,
		)
		if errors.Is(err, os.ErrNotExist) {
			return ownedFileCleanupResult{
				fileRemoved:  true,
				ancillaryErr: directoryErr,
			}
		}
		return classifyOwnedFileCleanupFailure(
			path,
			identity,
			fmt.Errorf(
				"atomically claim owned %s %q for cleanup: %w",
				label,
				path,
				err,
			),
			directoryErr,
			operations,
		)
	}

	claimedIdentity, err := operations.lstat(claimedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ownedFileCleanupResult{
				fileRemoved: true,
				ancillaryErr: errors.Join(
					fmt.Errorf(
						"claimed %s %q disappeared before identity verification",
						label,
						claimedPath,
					),
					removeEmptyOwnedFileCleanupDirectory(
						cleanupDirectory,
						operations,
					),
				),
			}
		}
		cleanupErr := restoreClaimedOwnedFile(
			path,
			claimedPath,
			cleanupDirectory,
			label,
			fmt.Errorf(
				"inspect claimed %s %q: %w",
				label,
				claimedPath,
				err,
			),
			operations,
		)
		return classifyOwnedFileCleanupFailure(
			path,
			identity,
			cleanupErr,
			nil,
			operations,
		)
	}
	if !operations.sameFile(identity, claimedIdentity) {
		return ownedFileCleanupResult{
			err: restoreClaimedOwnedFile(
				path,
				claimedPath,
				cleanupDirectory,
				label,
				fmt.Errorf(
					"refuse to remove %s %q because its atomically claimed pathname no longer refers to this invocation's file",
					label,
					path,
				),
				operations,
			),
		}
	}

	if removeErr := operations.remove(claimedPath); removeErr != nil &&
		!errors.Is(removeErr, os.ErrNotExist) {
		currentIdentity, inspectErr := operations.lstat(claimedPath)
		if errors.Is(inspectErr, os.ErrNotExist) {
			return ownedFileCleanupResult{
				fileRemoved: true,
				ancillaryErr: errors.Join(
					fmt.Errorf(
						"remove claimed %s %q returned an error after the pathname disappeared: %w",
						label,
						claimedPath,
						removeErr,
					),
					removeEmptyOwnedFileCleanupDirectory(
						cleanupDirectory,
						operations,
					),
				),
			}
		}
		if inspectErr != nil {
			return ownedFileCleanupResult{
				err: errors.Join(
					fmt.Errorf(
						"remove claimed %s %q: %w",
						label,
						claimedPath,
						removeErr,
					),
					fmt.Errorf(
						"inspect claimed %s %q after removal error: %w",
						label,
						claimedPath,
						inspectErr,
					),
				),
			}
		}
		if !operations.sameFile(identity, currentIdentity) {
			return ownedFileCleanupResult{
				err: restoreClaimedOwnedFile(
					path,
					claimedPath,
					cleanupDirectory,
					label,
					fmt.Errorf(
						"remove claimed %s %q returned an error and the claim now refers to a different file: %w",
						label,
						claimedPath,
						removeErr,
					),
					operations,
				),
			}
		}
		cleanupErr := restoreClaimedOwnedFile(
			path,
			claimedPath,
			cleanupDirectory,
			label,
			fmt.Errorf(
				"remove claimed %s %q: %w",
				label,
				claimedPath,
				removeErr,
			),
			operations,
		)
		return classifyOwnedFileCleanupFailure(
			path,
			identity,
			cleanupErr,
			nil,
			operations,
		)
	}
	return ownedFileCleanupResult{
		fileRemoved: true,
		ancillaryErr: removeEmptyOwnedFileCleanupDirectory(
			cleanupDirectory,
			operations,
		),
	}
}

func classifyOwnedFileCleanupFailure(
	path string,
	identity os.FileInfo,
	cleanupErr error,
	ancillaryErr error,
	operations ownedFileCleanupOperations,
) ownedFileCleanupResult {
	currentIdentity, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ownedFileCleanupResult{
			fileRemoved:  true,
			ancillaryErr: errors.Join(cleanupErr, ancillaryErr),
		}
	}
	if err != nil {
		return ownedFileCleanupResult{
			err: errors.Join(
				cleanupErr,
				fmt.Errorf(
					"inspect %q after cleanup failure: %w",
					path,
					err,
				),
			),
			ancillaryErr: ancillaryErr,
		}
	}
	if operations.sameFile(identity, currentIdentity) {
		return ownedFileCleanupResult{
			fileRetained: true,
			err:          cleanupErr,
			ancillaryErr: ancillaryErr,
		}
	}
	return ownedFileCleanupResult{
		err: errors.Join(
			cleanupErr,
			fmt.Errorf(
				"cleanup path %q no longer refers to the expected file",
				path,
			),
		),
		ancillaryErr: ancillaryErr,
	}
}

func restoreClaimedOwnedFile(
	originalPath string,
	claimedPath string,
	cleanupDirectory string,
	label string,
	cleanupErr error,
	operations ownedFileCleanupOperations,
) error {
	if err := operations.renameNoReplace(claimedPath, originalPath); err != nil {
		return errors.Join(
			cleanupErr,
			fmt.Errorf(
				"restore claimed %s to %q without clobbering; retained at %q: %w",
				label,
				originalPath,
				claimedPath,
				err,
			),
		)
	}
	return errors.Join(
		fmt.Errorf(
			"%w; restored the claimed file to %q without clobbering",
			cleanupErr,
			originalPath,
		),
		removeEmptyOwnedFileCleanupDirectory(
			cleanupDirectory,
			operations,
		),
	)
}

func removeEmptyOwnedFileCleanupDirectory(
	path string,
	operations ownedFileCleanupOperations,
) error {
	if err := operations.remove(path); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"remove empty private cleanup directory %q: %w",
			path,
			err,
		)
	}
	return nil
}

func removeOwnedFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove owned temporary file %q: %w", path, err)
	}
	return nil
}
