#!/bin/sh
set -eu

if [ "${1:-}" = "schedule" ]; then
    shift

    if [ "$#" -gt 1 ]; then
        printf '%s\n' "usage: gopod-entrypoint schedule [crontab-path]" >&2
        exit 64
    fi

    crontab_path="${1:-${GOPOD_CRONTAB:-/config/crontab}}"
    if [ ! -e "$crontab_path" ]; then
        printf 'gopod: crontab not found: %s\n' "$crontab_path" >&2
        exit 66
    fi
    if [ ! -f "$crontab_path" ]; then
        printf 'gopod: crontab is not a regular file: %s\n' "$crontab_path" >&2
        exit 66
    fi
    if [ ! -r "$crontab_path" ]; then
        printf 'gopod: crontab is not readable: %s\n' "$crontab_path" >&2
        exit 77
    fi

    exec /usr/local/bin/supercronic -inotify "$crontab_path"
fi

exec /usr/local/bin/gopod --config "${GOPOD_CONFIG:-/config/config.json}" "$@"
