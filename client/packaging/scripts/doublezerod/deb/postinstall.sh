#!/bin/sh

set -e

if ! getent group doublezero >/dev/null 2>&1; then
    addgroup --system --quiet doublezero
fi
if ! getent passwd doublezero >/dev/null 2>&1; then
    adduser --system --quiet --ingroup doublezero          \
            --no-create-home --home /nonexistent        \
            doublezero
fi

if [ "$1" = "configure" ] || [ "$1" = "abort-upgrade" ] || [ "$1" = "abort-deconfigure" ] || [ "$1" = "abort-remove" ] ; then
	deb-systemd-helper unmask 'doublezerod.service' >/dev/null || true
	if deb-systemd-helper --quiet was-enabled 'doublezerod.service'; then
		deb-systemd-helper enable 'doublezerod.service' >/dev/null || true
	else
		deb-systemd-helper update-state 'doublezerod.service' >/dev/null || true
	fi

	if [ -d /run/systemd/system ]; then
		systemctl --system daemon-reload >/dev/null || true
		deb-systemd-invoke restart 'doublezerod.service' >/dev/null || true
	fi
fi
