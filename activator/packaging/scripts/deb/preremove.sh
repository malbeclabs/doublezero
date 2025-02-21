#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
	if [ -d /run/systemd/system ]; then
		deb-systemd-invoke stop 'doublezero-activator.service' >/dev/null || true
	fi
fi
