#!/bin/sh

set -e

if ! getent group doublezero >/dev/null 2>&1; then
    groupadd --system doublezero
fi
if ! getent passwd doublezero >/dev/null 2>&1; then
    useradd --system -g doublezero --no-create-home doublezero
fi

if [ $1 -eq 1 ] && [ -x "/usr/lib/systemd/systemd-update-helper" ]; then 
    # Initial installation 
    /usr/lib/systemd/systemd-update-helper install-system-units doublezerod.service || : 
fi
if [ -d /run/systemd/system ]; then
    systemctl --system daemon-reload >/dev/null || true
    systemctl restart 'doublezerod.service' >/dev/null || true
fi

