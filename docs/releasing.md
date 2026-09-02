# Releasing GoPod

GoPod releases are created from semantic-version tags on `main`. The release workflow publishes native archives to GitHub Releases and a multi-platform container image to GitHub Container Registry (GHCR).

## Prepare a release

1. Update `main` and start from a clean checkout.
2. Install the pinned project tools:

   ```sh
   mise install
   ```

3. Run the same checks used by CI:

   ```sh
   mise run ci
   mise run release:snapshot
   ```

4. Inspect `dist/`. A snapshot should contain archives for:

   - Linux amd64
   - Linux arm64
   - macOS arm64
   - Windows amd64

   Native binaries require `ffmpeg` to be installed separately.

## Publish

After the release commit is merged to `main`, create and push an annotated semantic-version tag:

```sh
git switch main
git pull --ff-only
git tag -a v0.1.0 -m "GoPod v0.1.0"
git push origin v0.1.0
```

The workflow rejects tags that are not valid semantic versions or do not point to a commit on `main`. Pre-release tags such as `v1.2.0-rc.1` are marked as prereleases and do not update the moving `latest`, major, or minor container tags. Build-metadata suffixes such as `+build.1` are intentionally not accepted because container tags cannot represent them without rewriting.

Publish stable versions in ascending order so moving container tags cannot regress. Do not move or reuse a published tag; fix release problems with a new patch version.

Before publishing, the tag workflow reruns the complete mise suite, builds all native snapshot targets, builds both container architectures, loads a release-candidate image, and runs the same image/Compose smoke checks used by pull requests.

Publication is staged:

1. GoReleaser creates or resumes a draft GitHub Release and uploads native assets.
2. The workflow publishes the immutable `vX.Y.Z` image tag and records its registry digest in `container-digest.txt`.
3. The draft is published after the immutable image succeeds.
4. A serialized job promotes stable releases to the moving major, minor, and `latest` image tags and marks the highest successfully published stable version as GitHub's latest release.

This ordering keeps existing production tags unchanged if validation or immutable publication fails. If only moving-tag promotion fails or is canceled behind another promotion, rerun that job. A complete release rerun verifies the immutable image against the digest stored with the GitHub Release before reusing it.

## Published artifacts

The GitHub Release contains compressed native binaries, release notes, `checksums.txt`, and `container-digest.txt`. Verify an archive after download with the platform's SHA-256 tool, for example:

```sh
sha256sum --check checksums.txt
```

The workflow also publishes:

```text
ghcr.io/djensenius/gopod:vX.Y.Z
ghcr.io/djensenius/gopod:X.Y
ghcr.io/djensenius/gopod:X
ghcr.io/djensenius/gopod:latest
```

Moving major, minor, and `latest` tags are created only for stable releases.

## First GHCR publication

Container package visibility is separate from repository visibility. After the first image is published, open the `gopod` package settings on GitHub and set its visibility to public so Portainer, Kubernetes, and Docker can pull it anonymously.

The image includes an OCI source label that links the package to this repository.

## Verify a release

1. Confirm the GitHub Release contains exactly four platform archives plus `checksums.txt` and `container-digest.txt`.
2. Extract one archive and run `gopod --version`; it should report the released version and commit.
3. Inspect the image manifest:

   ```sh
   docker buildx imagetools inspect ghcr.io/djensenius/gopod:vX.Y.Z
   ```

4. Pull and inspect the image:

   ```sh
   docker run --rm ghcr.io/djensenius/gopod:vX.Y.Z --version
   docker run --rm --entrypoint ffmpeg ghcr.io/djensenius/gopod:vX.Y.Z -version
   ```

5. Run a short test recording with temporary mounted config and data directories before updating production deployments.

The implementation workflow does not create the first tag automatically. Publishing the first release is a separate, deliberate operation after the release pipeline has merged.
