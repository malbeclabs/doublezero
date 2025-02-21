#!/bin/sh
if [ $1 -eq 0 ] && [ -x "/usr/lib/systemd/systemd-update-helper" ]; then 
    # Package removal, not upgrade 
    /usr/lib/systemd/systemd-update-helper remove-system-units doublezerod.service || : 
fi
if [ -d /run/systemd/system ]; then
    systemctl stop 'doublezerod.service' >/dev/null || true
fi
