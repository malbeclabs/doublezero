#!/bin/sh
if [ "$1" = "configure" ] || [ "$1" = "abort-upgrade" ] || [ "$1" = "abort-deconfigure" ] || [ "$1" = "abort-remove" ] ; then
	deb-systemd-helper unmask 'doublezero-qaagent.service' >/dev/null || true
	if deb-systemd-helper --quiet was-enabled 'doublezero-qaagent.service'; then
		deb-systemd-helper enable 'doublezero-qaagent.service' >/dev/null || true
	else
		deb-systemd-helper update-state 'doublezero-qaagent.service' >/dev/null || true
	fi

	if [ -d /run/systemd/system ]; then
		systemctl --system daemon-reload >/dev/null || true
		deb-systemd-invoke restart 'doublezero-qaagent.service' >/dev/null || true
	fi
fi
