package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"gopod/podcast"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	configPath string
	shortTitle string
	version    bool
}

type monitorResult struct {
	metadataFile    string
	descriptionFile string
	err             error
}

type recordResult struct {
	recordedFile string
	err          error
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("gopod", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.configPath, "config", "", "path to the JSON configuration file")
	flags.BoolVar(&opts.version, "version", false, "print version information")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: gopod [--config path] <short-title>")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Options:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	positional := flags.Args()
	if len(positional) > 1 {
		return options{}, fmt.Errorf("expected one podcast short title, got %d", len(positional))
	}
	if len(positional) == 1 {
		opts.shortTitle = positional[0]
	}

	return opts, nil
}

func resolveConfigPath(
	explicitPath string,
	lookupEnv func(string) (string, bool),
	userConfigDir func() (string, error),
	userHomeDir func() (string, error),
	stat func(string) (os.FileInfo, error),
) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	if envPath, ok := lookupEnv("GOPOD_CONFIG"); ok && envPath != "" {
		return envPath, nil
	}

	configDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default config path: %w", err)
	}
	nativePath := filepath.Join(configDir, "gopod", "config.json")
	if _, err := stat(nativePath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nativePath, nil
	}

	homeDir, err := userHomeDir()
	if err != nil || homeDir == "" {
		return nativePath, nil
	}
	legacyPath := filepath.Join(homeDir, ".config", "gopod", "config.json")
	if _, err := stat(legacyPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return legacyPath, nil
	}
	return nativePath, nil
}

func loadConfig(path string) (podcast.Podcasts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return podcast.Podcasts{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var podcasts podcast.Podcasts
	if err := json.Unmarshal(data, &podcasts); err != nil {
		return podcast.Podcasts{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	for index, configured := range podcasts.Podcasts {
		if configured == nil {
			continue
		}
		if err := podcast.ValidateShortTitle(configured.ShortTitle); err != nil {
			return podcast.Podcasts{}, fmt.Errorf(
				"validate config %q: podcast %d %q has invalid ShortTitle: %w",
				path,
				index+1,
				configured.Title,
				err,
			)
		}
	}
	return podcasts, nil
}

func versionOutput() string {
	return fmt.Sprintf("gopod %s (commit %s, built %s)", version, commit, date)
}

func ensureOutputDirectory(path string) error {
	if path == "" {
		return errors.New("podcast output directory must not be empty")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create podcast output directory %q: %w", path, err)
	}
	return nil
}

func recordPodcast(ctx context.Context, selected podcast.Podcast) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	lock, err := acquirePodcastLock(selected)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.Close(); releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("release podcast lock: %w", releaseErr))
		}
	}()

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	monitorResults := make(chan monitorResult, 1)
	recordResults := make(chan recordResult, 1)

	go func() {
		metadataFile, descriptionFile, err := podcast.MonitorStream(
			workerCtx,
			selected.SourceURL,
			time.Duration(selected.Length)*time.Second,
			selected.Title,
		)
		monitorResults <- monitorResult{
			metadataFile:    metadataFile,
			descriptionFile: descriptionFile,
			err:             err,
		}
	}()

	go func() {
		recordedFile, err := podcast.Record(workerCtx, selected)
		recordResults <- recordResult{recordedFile: recordedFile, err: err}
	}()

	var monitored monitorResult
	var recorded recordResult
	var firstErr error
	for completed := 0; completed < 2; completed++ {
		select {
		case monitored = <-monitorResults:
			if monitored.err != nil && firstErr == nil {
				firstErr = fmt.Errorf("monitor podcast stream: %w", monitored.err)
				cancel()
			}
		case recorded = <-recordResults:
			if recorded.err != nil && firstErr == nil {
				firstErr = fmt.Errorf("record podcast: %w", recorded.err)
				cancel()
			}
		}
	}

	defer func() {
		for _, path := range []string{
			recorded.recordedFile,
			monitored.metadataFile,
			monitored.descriptionFile,
		} {
			if path == "" {
				continue
			}
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary file %q: %w", path, removeErr))
			}
		}
	}()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if firstErr != nil {
		return firstErr
	}

	if err := podcast.Combine(
		ctx,
		selected,
		recorded.recordedFile,
		monitored.metadataFile,
		monitored.descriptionFile,
	); err != nil {
		return fmt.Errorf("combine podcast: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := podcast.GeneratePodcastFeed(selected); err != nil {
		return fmt.Errorf("generate podcast feed: %w", err)
	}
	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	if opts.version {
		_, err := fmt.Fprintln(stdout, versionOutput())
		return err
	}
	if opts.shortTitle == "" {
		return errors.New("no podcast specified")
	}

	configPath, err := resolveConfigPath(
		opts.configPath,
		os.LookupEnv,
		os.UserConfigDir,
		os.UserHomeDir,
		os.Stat,
	)
	if err != nil {
		return err
	}
	podcasts, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	selected, err := podcast.FindPodcast(opts.shortTitle, podcasts)
	if err != nil {
		return fmt.Errorf(
			"find podcast %q in config %q: %w",
			opts.shortTitle,
			configPath,
			err,
		)
	}
	if err := ensureOutputDirectory(selected.Directory); err != nil {
		return err
	}
	if err := recordPodcast(ctx, selected); err != nil {
		return err
	}

	const (
		reset = "\033[0m"
		green = "\033[32m"
	)
	_, err = fmt.Fprintln(stdout, "\n"+green+"✅ Podcast feed generated"+reset)
	return err
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}
