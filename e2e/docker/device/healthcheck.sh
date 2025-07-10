#!/bin/bash
set -e

if [ -n "${DZ_MANAGEMENT_NAMESPACE}" ]; then
  ip netns exec ${DZ_MANAGEMENT_NAMESPACE} curl --silent --fail http://localhost/ || exit 1
else
  curl --silent --fail http://localhost/ || exit 1
fi
