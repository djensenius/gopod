package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopod/podcast"
)

func TestResolveConfigPathPrecedence(t *testing.T) {
	t.Run("explicit flag wins", func(t *testing.T) {
		path, err := resolveConfigPath(
			"/flag/config.json",
			func(string) (string, bool) { return "/env/config.json", true },
			func() (string, error) { panic("user config directory should not be read") },
			func() (string, error) { panic("home directory should not be read") },
			func(string) (os.FileInfo, error) { panic("filesystem should not be read") },
		)
		if err != nil {
			t.Fatal(err)
		}
		if path != "/flag/config.json" {
			t.Fatalf("got %q, want explicit config path", path)
		}
	})

	t.Run("environment wins over default", func(t *testing.T) {
		path, err := resolveConfigPath(
			"",
			func(string) (string, bool) { return "/env/config.json", true },
			func() (string, error) { panic("user config directory should not be read") },
			func() (string, error) { panic("home directory should not be read") },
			func(string) (os.FileInfo, error) { panic("filesystem should not be read") },
		)
		if err != nil {
			t.Fatal(err)
		}
		if path != "/env/config.json" {
			t.Fatalf("got %q, want environment config path", path)
		}
	})

	t.Run("native path wins when both exist", func(t *testing.T) {
		configDir := t.TempDir()
		homeDir := t.TempDir()
		nativePath := filepath.Join(configDir, "gopod", "config.json")
		legacyPath := filepath.Join(homeDir, ".config", "gopod", "config.json")
		writeTestFile(t, nativePath, "native")
		writeTestFile(t, legacyPath, "legacy")

		path, err := resolveConfigPath(
			"",
			func(string) (string, bool) { return "", false },
			func() (string, error) { return configDir, nil },
			func() (string, error) { return homeDir, nil },
			os.Stat,
		)
		if err != nil {
			t.Fatal(err)
		}
		if path != nativePath {
			t.Fatalf("got %q, want native path %q", path, nativePath)
		}
	})

	t.Run("legacy path is used when native path is missing", func(t *testing.T) {
		configDir := t.TempDir()
		homeDir := t.TempDir()
		legacyPath := filepath.Join(homeDir, ".config", "gopod", "config.json")
		writeTestFile(t, legacyPath, "legacy")

		path, err := resolveConfigPath(
			"",
			func(string) (string, bool) { return "", false },
			func() (string, error) { return configDir, nil },
			func() (string, error) { return homeDir, nil },
			os.Stat,
		)
		if err != nil {
			t.Fatal(err)
		}
		if path != legacyPath {
			t.Fatalf("got %q, want legacy path %q", path, legacyPath)
		}
	})

	t.Run("missing defaults return the native path", func(t *testing.T) {
		configDir := t.TempDir()
		homeDir := t.TempDir()
		nativePath := filepath.Join(configDir, "gopod", "config.json")

		path, err := resolveConfigPath(
			"",
			func(string) (string, bool) { return "", false },
			func() (string, error) { return configDir, nil },
			func() (string, error) { return homeDir, nil },
			os.Stat,
		)
		if err != nil {
			t.Fatal(err)
		}
		if path != nativePath {
			t.Fatalf("got %q, want missing native path %q", path, nativePath)
		}
	})
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("decodes valid config", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		data := []byte(`{"Podcasts":[{"ShortTitle":"rf","Title":"Rare Frequency","Directory":"/tmp/rf"}]}`)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		config, err := loadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(config.Podcasts) != 1 {
			t.Fatalf("got %d podcasts, want 1", len(config.Podcasts))
		}
		if config.Podcasts[0].ShortTitle != "rf" {
			t.Fatalf("got short title %q, want %q", config.Podcasts[0].ShortTitle, "rf")
		}
	})

	t.Run("reports missing path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected missing config error")
		}
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error %q does not include path %q", err, path)
		}
		if !strings.Contains(err.Error(), "read config") {
			t.Fatalf("error %q does not identify a read failure", err)
		}
	})

	t.Run("reports invalid path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(`{"Podcasts":`), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected invalid config error")
		}
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error %q does not include path %q", err, path)
		}
		if !strings.Contains(err.Error(), "decode config") {
			t.Fatalf("error %q does not identify a decode failure", err)
		}
	})

	t.Run("rejects reserved short title", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		data := []byte(
			`{"Podcasts":[{"Title":"Reserved Show","ShortTitle":".gopod-show"}]}`,
		)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected reserved short title error")
		}
		for _, want := range []string{
			path,
			"Reserved Show",
			"ShortTitle",
			".gopod-show",
			`reserved prefix ".gopod-"`,
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not contain %q", err, want)
			}
		}
	})
}

func TestParseOptionsPreservesLegacyPositionalInvocation(t *testing.T) {
	opts, err := parseOptions([]string{"rf"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.shortTitle != "rf" {
		t.Fatalf("got short title %q, want %q", opts.shortTitle, "rf")
	}
	if opts.configPath != "" {
		t.Fatalf("got unexpected config path %q", opts.configPath)
	}
	if opts.version {
		t.Fatal("version flag unexpectedly set")
	}
}

func TestParseOptionsAcceptsFlagsBeforeShortTitle(t *testing.T) {
	opts, err := parseOptions(
		[]string{"--config", "/tmp/gopod.json", "rf"},
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if opts.configPath != "/tmp/gopod.json" {
		t.Fatalf("got config path %q, want %q", opts.configPath, "/tmp/gopod.json")
	}
	if opts.shortTitle != "rf" {
		t.Fatalf("got short title %q, want %q", opts.shortTitle, "rf")
	}
}

func TestRunVersionRequiresNoConfig(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "1.2.3", "abc123", "2026-08-31T12:00:00Z"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := "gopod 1.2.3 (commit abc123, built 2026-08-31T12:00:00Z)\n"
	if stdout.String() != want {
		t.Fatalf("got %q, want %q", stdout.String(), want)
	}
}

func TestRunHelpRequiresNoConfig(t *testing.T) {
	var stderr bytes.Buffer
	err := run(context.Background(), []string{"--help"}, &bytes.Buffer{}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("got error %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "Usage: gopod") {
		t.Fatalf("help output %q does not contain usage", stderr.String())
	}
}

func TestRunReportsNoPodcast(t *testing.T) {
	err := run(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected no-podcast error")
	}
	if err.Error() != "no podcast specified" {
		t.Fatalf("got error %q, want %q", err, "no podcast specified")
	}
}

func TestRunReportsUnknownPodcastWithoutRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"Podcasts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run(
		context.Background(),
		[]string{"--config", path, "missing"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatal("expected unknown-podcast error")
	}
	if !strings.Contains(err.Error(), `find podcast "missing"`) {
		t.Fatalf("got error %q, want missing podcast name", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("got error %q, want config path %q", err, path)
	}
}

func TestRunRejectsReservedShortTitleBeforeOutputCreation(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "podcasts", "reserved")
	configured := podcast.Podcast{
		Title:      "Reserved Show",
		ShortTitle: ".gopod-show",
		Directory:  outputDir,
	}
	data, err := json.Marshal(podcast.Podcasts{
		Podcasts: []*podcast.Podcast{&configured},
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = run(
		ctx,
		[]string{"--config", configPath, configured.ShortTitle},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatal("expected reserved short title error")
	}
	if !strings.Contains(err.Error(), configured.Title) ||
		!strings.Contains(err.Error(), configured.ShortTitle) ||
		!strings.Contains(err.Error(), `reserved prefix ".gopod-"`) {
		t.Fatalf("error %q does not identify the reserved podcast ShortTitle", err)
	}
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output directory %q was created before validation: %v", outputDir, statErr)
	}
}

func TestEnsureOutputDirectory(t *testing.T) {
	t.Run("creates nested directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "podcasts", "rf")
		if err := ensureOutputDirectory(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", path)
		}
	})

	t.Run("reports path on failure", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parent, "podcast")
		err := ensureOutputDirectory(path)
		if err == nil {
			t.Fatal("expected directory creation error")
		}
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error %q does not include path %q", err, path)
		}
	})
}
