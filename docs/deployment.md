# Container deployment

The GoPod image is optional. Native binaries continue to work as before when `ffmpeg` is installed on the host.

For native config discovery, explicit `--config` and then `GOPOD_CONFIG` take precedence. Otherwise, GoPod prefers the platform-native user config path and falls back to legacy `$HOME/.config/gopod/config.json` only when that preferred default is absent.

The container image includes GoPod, `ffmpeg`, CA certificates, timezone data, and Supercronic. It supports one-shot recordings and a long-running scheduler mode without exposing a network port.

## Image and mounts

Use an immutable release tag in production:

```text
ghcr.io/djensenius/gopod:vX.Y.Z
```

The image uses these paths:

| Path | Access | Purpose |
|---|---|---|
| `/config/config.json` | Read-only | GoPod podcast configuration |
| `/config/crontab` | Read-only | Optional Portainer/Supercronic schedule |
| `/data` | Read/write | Audio, descriptions, and generated RSS feeds |
| `/tmp` | Read/write | Temporary recording and metadata files |

Every `Directory` value in `config.json` must be a container path below `/data`, not the corresponding host path.

The image runs as a non-root user by default. Ensure the mounted data and scratch directories are writable by the configured container UID/GID.

## One-shot Docker use

The normal image entrypoint forwards arguments to GoPod:

```sh
docker run --rm \
  --read-only \
  --mount type=bind,src=/path/to/gopod/config,dst=/config,readonly \
  --mount type=bind,src=/path/to/podcasts,dst=/data \
  --mount type=bind,src=/path/to/gopod/tmp,dst=/tmp \
  ghcr.io/djensenius/gopod:vX.Y.Z \
  --config /config/config.json rf
```

Replace `rf` with the configured podcast's `ShortTitle`.

## Portainer

`deploy/portainer/compose.yaml` is a Portainer-compatible stack for long-running scheduler mode.

Use `deploy/portainer/.env.example` as the variable template:

| Variable | Compose requirement/default | Purpose |
|---|---|---|
| `GOPOD_IMAGE_TAG` | Optional; `v0.1.0` | Immutable release tag |
| `GOPOD_CONFIG_DIR` | Required | Directory containing `config.json` and `crontab` |
| `GOPOD_DATA_PATH` | Required | Podcast data directory |
| `GOPOD_TMP_PATH` | Required | Scratch directory for in-progress recordings |
| `GOPOD_UID` / `GOPOD_GID` | Optional; `10001` | UID/GID allowed to write the data and scratch directories |
| `GOPOD_STOP_GRACE_PERIOD` | Optional; `3h` | Shutdown grace period long enough to finish a recording |
| `TZ` | Optional; `Etc/UTC` | Container timezone |

How the variables reach Portainer depends on the stack deployment method:

- **Git repository:** define the variables in Portainer's stack environment-variable UI. A local `deploy/portainer/.env` is gitignored and is not sent to a Git-based stack.
- **Web editor or Upload:** copy `.env.example` to `deploy/portainer/.env`, populate it, and explicitly load/upload that file with Portainer's environment-file control when deploying the stack.

`GOPOD_CONFIG_DIR`, `GOPOD_DATA_PATH`, and `GOPOD_TMP_PATH` must be absolute bind-mount paths on the Docker host managed by Portainer, not paths on the machine running the browser or client.

Put `config.json` and `crontab` in `GOPOD_CONFIG_DIR`, create the data and scratch directories, adjust each crontab line to invoke the desired podcast short title, and deploy the Compose file as a Portainer stack.

The stack executes:

```text
schedule /config/crontab
```

The configuration directory is mounted read-only as `/config`, so atomic file replacements remain visible inside the container. Supercronic stays in the foreground, writes job output to container stdout/stderr, honors `TZ` and `CRON_TZ`, watches the crontab for changes, and handles container termination signals. Some Docker bind-mount backends do not propagate host rename events; if the logs do not show a reload after replacing `crontab`, restart the Portainer stack. Size the scratch directory for the largest expected source recording; it does not need to be backed up.

Example crontab:

```cron
CRON_TZ=America/New_York
59 21 * * 4 /usr/local/bin/gopod --config /config/config.json rf
59 19 * * 2 /usr/local/bin/gopod --config /config/config.json vf
```

Supercronic prevents overlapping executions of the same schedule. Keep each schedule interval longer than the configured recording length.

## Kubernetes

The examples in `deploy/kubernetes` use one Kubernetes `CronJob` per podcast schedule.

1. Replace the example stream and feed values in the config manifest.
2. Set an immutable image tag in `cronjob.yaml`.
3. Set the desired schedule, `timeZone`, and podcast short title.
4. Replace the example PVC storage class with a `ReadWriteMany`-capable class available in the cluster and adjust its size.
5. Render and validate before applying:

   ```sh
   mise run lint:kubernetes
   kubectl apply --dry-run=client -k deploy/kubernetes
   ```

6. Apply the resources:

   ```sh
   kubectl apply -k deploy/kubernetes
   ```

The sample mounts a shared `ReadWriteMany` PVC at `/data`, mounts configuration read-only, uses a 2 GiB node-backed `emptyDir` for `/tmp`, runs without root privileges, sets `concurrencyPolicy: Forbid`, and keeps `backoffLimit: 0` because recording is not idempotent. Inspect a failed job before rerunning it manually. Adjust the temporary-storage limit and `activeDeadlineSeconds` for longer or higher-bitrate recordings.

Treat Kubernetes CronJob execution as at-least-once for duplicate protection, not exactly-once: the controller can create more than one Job for a schedule, and some failure or deadline windows can also miss a run. `concurrencyPolicy: Forbid` prevents overlapping Jobs created by the same CronJob, while `backoffLimit: 0` disables retries within a Job; neither setting provides exactly-once execution.

GoPod's application-level per-podcast lock prevents simultaneous runs for the same podcast while one process holds the lock. It does not retain a completed schedule or run identity, so a delayed duplicate Job that starts after the first Job releases the lock can run and create another episode. Exact-once behavior requires an external persistent schedule/run identity or deduplication mechanism that spans Job lifetimes.

For multiple podcasts, copy the `CronJob`, give each copy a unique name, schedule, and short title, and reuse the config resource. Share the sample PVC only when its storage class supports `ReadWriteMany`, allowing Jobs scheduled on different nodes to mount it safely.

If the cluster has no suitable `ReadWriteMany` storage class, create a separate `ReadWriteOnce` PVC for each podcast and update each CronJob's `claimName`; do not share one `ReadWriteOnce` claim across CronJobs. `ReadWriteOnce` limits a volume to one node, not one writer, so it can mount-fail when Jobs land on different nodes and does not prevent concurrent writes by Pods on the same node.

With either layout, the persistent-volume backend and filesystem must provide coherent advisory locking across every Pod that can access a podcast's data, plus at least one atomic no-clobber publication capability: Linux `renameat2(..., RENAME_NOREPLACE)` or same-filesystem hard links. GoPod falls back to hard-link publication when no-replace rename is unsupported. `ReadWriteMany` access alone does not guarantee these semantics; verify the selected storage class and CSI/filesystem implementation.

The checked-in example uses a `ConfigMap` because its values are placeholders. If a production stream URL contains credentials, store the JSON in a Kubernetes `Secret` and mount it at the same `/config/config.json` path.

Inspect a job with:

```sh
kubectl get jobs
kubectl logs job/<job-name>
```

## Persistent data

Back up `/data`; it contains the recordings, generated descriptions, and `podcast.rss`. Configuration can be restored separately from the read-only `/config` mount.
