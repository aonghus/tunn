#!/usr/bin/env bash
# Creates the client key and the `tunn-smoke` ssh host alias used by the smoke test.
# Usage: setup-client.sh <sshd-port>
# CI only: it appends to ~/.ssh/config.
set -euo pipefail

: "${CI:?refusing to run outside CI: this script modifies ~/.ssh}"
port=${1:?usage: setup-client.sh <sshd-port>}

mkdir -p ~/.ssh
chmod 700 ~/.ssh
rm -f ~/.ssh/tunn_smoke ~/.ssh/tunn_smoke.pub
ssh-keygen -q -t ed25519 -N '' -C tunn-smoke -f ~/.ssh/tunn_smoke

cat >> ~/.ssh/config <<EOF

Host tunn-smoke
    HostName 127.0.0.1
    Port $port
    User $(whoami)
    IdentityFile ~/.ssh/tunn_smoke
    IdentitiesOnly yes
    BatchMode yes
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    ExitOnForwardFailure yes
    LogLevel ERROR
EOF
chmod 600 ~/.ssh/config
echo "ssh alias tunn-smoke -> 127.0.0.1:$port as $(whoami)"
