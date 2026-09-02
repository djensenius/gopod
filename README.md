# GoPod

GoPod records an online radio stream, captures Icecast track metadata, writes chapter and description data, and generates a podcast RSS feed from the resulting audio files.

## Requirements

- Native use: `ffmpeg` must be available on `PATH`.
- Development: [mise](https://mise.jdx.dev/) installs the pinned Go and validation tools.
- Container use: no host `ffmpeg` installation is required.

## Install from source

```sh
mise install
mise run build:binary
```

The binary is written to `./gopod`.

Tagged releases also publish prebuilt archives for Linux amd64, Linux arm64, macOS arm64, and Windows amd64. Native release archives still require a separate `ffmpeg` installation.

## Configure

GoPod resolves its JSON config in this order:

1. `--config <path>`
2. `GOPOD_CONFIG`
3. The preferred platform user config path:
   - Linux: `${XDG_CONFIG_HOME:-$HOME/.config}/gopod/config.json`
   - macOS: `~/Library/Application Support/gopod/config.json`
   - Windows: `%AppData%\gopod\config.json`
4. Legacy `$HOME/.config/gopod/config.json`, only when the preferred default is absent

Explicit `--config` and `GOPOD_CONFIG` paths are used as given; the legacy path participates only in default discovery.

Start from the sample:

```sh
cp config.json.sample config.json
```

The repository-root `/config.json` path is gitignored. Edit the copy, then pass it with `--config`, set `GOPOD_CONFIG`, or move it to the preferred platform config path.

Each object in `Podcasts` supports:

| Key | Description |
|---|---|
| `Directory` | Directory where audio, description, and RSS files are written |
| `Title` | Podcast title |
| `ShortTitle` | Short command-line identifier and filename prefix; `.gopod` and the `.gopod-` prefix are reserved for internal work files |
| `Image` | Podcast artwork URL |
| `URL` | Public base URL for generated audio enclosures |
| `PodcastURL` | Website URL for the podcast |
| `SourceURL` | Online audio stream URL |
| `Length` | Recording length in seconds |
| `Extension` | Source recording extension, typically `mp3` or `aac` |

Example:

```json
{
  "Podcasts": [
    {
      "Directory": "/srv/podcasts/rare-frequency",
      "Title": "Rare Frequency",
      "ShortTitle": "rf",
      "Image": "https://podcasts.example.com/rare-frequency/artwork.jpg",
      "URL": "https://podcasts.example.com/rare-frequency/",
      "PodcastURL": "https://www.example.com/rare-frequency",
      "SourceURL": "https://stream.example.com/rare-frequency.aac",
      "Length": 3600,
      "Extension": "aac"
    }
  ]
}
```

GoPod creates the configured output directory when needed. Container configs must use container paths below `/data`, not host filesystem paths.

## Run

Record the podcast whose `ShortTitle` is `rf`:

```sh
./gopod rf
```

Use a specific config:

```sh
./gopod --config /path/to/config.json rf
```

Show build information:

```sh
./gopod --version
```

The recording, matching description file, `podcast.rss`, and hidden `.gopod-*` publication controls are written to the podcast's configured directory. Preserve those hidden controls when copying or backing up the directory; GoPod uses them to exclude incomplete recordings from regenerated feeds.

For native scheduling, invoke the same one-shot command from cron or another host scheduler:

```cron
59 21 * * 4 /usr/local/bin/gopod --config /etc/gopod/config.json rf
```

## Containers

Release images are published to:

```text
ghcr.io/djensenius/gopod:vX.Y.Z
```

The image supports:

- one-shot `docker run` commands;
- a long-running Supercronic mode for Portainer; and
- one-shot Kubernetes `CronJob` resources.

See [Container deployment](docs/deployment.md) for mount paths, Portainer Compose setup, Kubernetes examples, permissions, and timezone behavior.

## Development

The committed mise configuration is the source of truth for tool versions and tasks:

```sh
mise install
mise run ci
mise run release:snapshot
```

Task orchestration uses Bash. On Windows, use Git Bash or the provided devcontainer; the released Windows binary itself has no Bash dependency.

Useful tasks are listed with:

```sh
mise tasks ls
```

## Releases

Semantic-version tags publish native archives, checksums, release notes, and multi-platform GHCR images. See [Releasing GoPod](docs/releasing.md) for the release and verification process.

## License

GoPod is released under the terms in [LICENSE](LICENSE).
