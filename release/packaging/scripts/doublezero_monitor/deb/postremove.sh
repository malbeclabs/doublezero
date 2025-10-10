#!/bin/sh
set -e

DIR_PATH="/opt/doublezero_monitor"

case "$1" in
    purge)
        echo "Removing directory $DIR_PATH..."
        if [ -d "$DIR_PATH" ]; then
            rmdir --ignore-fail-on-non-empty "$DIR_PATH" || true
        fi
    ;;

    remove|upgrade|failed-upgrade|abort-install|abort-upgrade)
    ;;

    *)
        echo "postrm called with unknown argument '$1'" >&2
        exit 1
    ;;
esac

exit 0
