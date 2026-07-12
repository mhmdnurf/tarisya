#!/bin/bash
set -a
# shellcheck disable=SC1091
source "/Library/Application Support/Tarisya/agent.env"
set +a
exec /usr/local/bin/tarisya-agent
