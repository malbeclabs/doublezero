#!/usr/bin/env bash
set -euo pipefail

set -x
docker ps -aq --filter label=org.testcontainers=true | xargs -r docker rm -f
docker volume ls -q --filter label=org.testcontainers=true | xargs -r docker volume rm -f
docker network ls -q --filter label=org.testcontainers=true | xargs -r docker network rm -f
