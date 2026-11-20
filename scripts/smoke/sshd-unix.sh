#!/usr/bin/env bash
# Starts an unprivileged sshd on 127.0.0.1:<port> that accepts ~/.ssh/tunn_smoke.pub.
# Usage: sshd-unix.sh <port>   (run setup-client.sh first)
set -euo pipefail

port=${1:?usage: sshd-unix.sh <port>}
dir=$(mktemp -d)

ssh-keygen -q -t ed25519 -N '' -f "$dir/host_key"
cp ~/.ssh/tunn_smoke.pub "$dir/authorized_keys"
chmod 600 "$dir/authorized_keys"

cat > "$dir/sshd_config" <<EOF
Port $port
ListenAddress 127.0.0.1
HostKey $dir/host_key
PidFile $dir/sshd.pid
AuthorizedKeysFile $dir/authorized_keys
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
StrictModes no
AllowTcpForwarding yes
EOF

# Debian/Ubuntu sshd refuses to start without its privilege-separation directory.
if [ "$(uname -s)" = Linux ] && [ ! -d /run/sshd ]; then
  sudo mkdir -p /run/sshd
fi

/usr/sbin/sshd -f "$dir/sshd_config" -E "$dir/sshd.log"
echo "sshd listening on 127.0.0.1:$port (log: $dir/sshd.log)"
