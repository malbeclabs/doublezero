#!/bin/sh
set -e

DIR_PATH="/opt/doublezero_monitor"

case "$1" in
    install|upgrade)
        echo "Creating directory $DIR_PATH..."
        mkdir -p "$DIR_PATH"
    ;;

    abort-upgrade)
    ;;

    *)
        echo "preinst called with unknown argument '$1'" >&2
        exit 1
    ;;
esac

exit 0
