#!/usr/bin/env bash
# End-to-end smoke test: builds tunn and drives it against a real sshd through
# the `tunn-smoke` ssh alias (see setup-client.sh and sshd-*.{sh,ps1}).
#
# Covers: detach, status over the IPC socket, traffic through a -L forward,
# the "already running" guard, stop, and port-conflict detection.
# Each check is reported separately and the script fails if any of them fail.
#
# CI only: it overwrites ~/.tunnrc.
set -u

: "${CI:?refusing to run outside CI: this script overwrites ~/.tunnrc}"

LOCAL_PORT=18080   # tunn listens here
REMOTE_PORT=18081  # http backend, reached through sshd
BUSY_PORT=18082    # held by another process to provoke a conflict

failures=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; failures=$((failures + 1)); }

is_windows() {
  case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) return 0 ;; esac
  return 1
}

pid_alive() {
  if is_windows; then
    tasklist //FI "PID eq $1" //NH 2>/dev/null | grep -qw "$1"
  else
    kill -0 "$1" 2>/dev/null
  fi
}

kill_tree() {
  if is_windows; then
    taskkill //F //T //PID "$1" >/dev/null 2>&1
  else
    pkill -P "$1" 2>/dev/null
    kill "$1" 2>/dev/null
  fi
}

# wait_for <seconds> <command...>: retry until the command succeeds or time runs out.
wait_for() {
  local end=$((SECONDS + $1))
  shift
  until "$@"; do
    [ "$SECONDS" -ge "$end" ] && return 1
    sleep 0.5
  done
}

fetch_marker() { [ "$(curl -fsS --max-time 3 "http://127.0.0.1:$LOCAL_PORT/marker.txt" 2>/dev/null)" = tunn-smoke-ok ]; }
port_closed() { ! curl -fsS --max-time 2 "http://127.0.0.1:$LOCAL_PORT/marker.txt" >/dev/null 2>&1; }
status_says() { "$TUNN" status 2>&1 | grep -q -- "$1"; }
daemon_dead() { ! pid_alive "$1"; }
daemon_pid() { "$TUNN" status 2>/dev/null | sed -n 's/.*(pid \([0-9][0-9]*\).*/\1/p' | head -n1; }

# Narrows down a failed forward: tunn vs. ssh vs. the backend, and whether
# the remote side's "localhost" resolving to ::1 matters.
diagnose_forward() {
  echo "----- diagnostics -----"
  "$TUNN" status 2>&1
  echo "# backend directly:"
  curl -sS --max-time 3 "http://127.0.0.1:$REMOTE_PORT/marker.txt" 2>&1
  echo "# through tunn's forward:"
  curl -sS --max-time 3 "http://127.0.0.1:$LOCAL_PORT/marker.txt" 2>&1
  if is_windows; then
    netstat -ano | grep -E ":(18080|18081|1809[0-9]) "
  fi
  local port=18090 target
  for target in localhost 127.0.0.1; do
    echo "# manual ssh -L $port:$target:$REMOTE_PORT:"
    ssh -v -N -L "$port:$target:$REMOTE_PORT" tunn-smoke >"$work/ssh-$port.log" 2>&1 &
    local sp=$!
    sleep 3
    curl -sS --max-time 3 "http://127.0.0.1:$port/marker.txt" 2>&1
    kill_tree "$sp"
    grep -vE "^debug1: (Reading|Applying|identity file|Local version|Remote protocol|compat|kex|SSH2_MSG|Server host key|load_hostkeys|Will attempt|Offering|Server accepts|Authentication succeeded|channel 0: new|Requesting|Entering|pledge|client_input|Sending|expecting|rekey|Remote: |Next authentication|Authentications that|Connecting to|Connection established|checking match|configuration requests|hostkeys_find)" "$work/ssh-$port.log" | tail -n 25
    port=$((port + 1))
  done
  echo "-----------------------"
}

work=$(mktemp -d)
bg_pids=()
daemon_pids=()

cleanup() {
  "$TUNN" stop >/dev/null 2>&1
  for p in "${daemon_pids[@]:-}" "${bg_pids[@]:-}"; do
    [ -n "$p" ] && kill_tree "$p"
  done
  if [ -n "${log_file:-}" ] && [ -f "$log_file" ]; then
    echo "----- daemon log -----"
    cat "$log_file"
  fi
}
trap cleanup EXIT

# --- setup -------------------------------------------------------------------

repo=$(cd "$(dirname "$0")/../.." && pwd)
(cd "$repo" && go build -o "$work/bin/" .) || { echo "build failed"; exit 1; }
TUNN="$work/bin/tunn"

if is_windows; then
  home=$(cygpath -u "$USERPROFILE")
else
  home=$HOME
fi
log_file="$home/.cache/tunn/daemon.log"
[ -n "${XDG_RUNTIME_DIR:-}" ] && log_file="$XDG_RUNTIME_DIR/tunn/daemon.log"

PY=
for c in python3 python; do
  if "$c" -c 'import sys' >/dev/null 2>&1; then PY=$c; break; fi
done
[ -n "$PY" ] || { echo "python not found"; exit 1; }

echo "os: $(uname -s)  ssh: $(command -v ssh) ($(ssh -V 2>&1))  python: $PY"

if ! ssh -o ConnectTimeout=10 tunn-smoke exit; then
  echo "ssh to tunn-smoke failed: the test sshd is not set up correctly"
  exit 2
fi

mkdir -p "$work/www"
echo tunn-smoke-ok > "$work/www/marker.txt"
(cd "$work/www" && exec "$PY" -m http.server "$REMOTE_PORT" --bind 127.0.0.1) >"$work/http.log" 2>&1 &
bg_pids+=($!)

cat > "$home/.tunnrc" <<EOF
tunnels:
  smoke:
    host: tunn-smoke
    ports:
      - "$LOCAL_PORT:$REMOTE_PORT"
  conflict:
    host: tunn-smoke
    ports:
      - "$BUSY_PORT:$REMOTE_PORT"
EOF

# --- detach, status, traffic -------------------------------------------------

if out=$("$TUNN" --detach smoke 2>&1); then
  pass "tunn --detach smoke"
else
  fail "tunn --detach smoke exited non-zero: $out"
fi
echo "$out"

if wait_for 10 status_says "$LOCAL_PORT:$REMOTE_PORT - active"; then
  pass "tunn status reports the tunnel active"
else
  fail "tunn status did not report the tunnel active"
fi
"$TUNN" status 2>&1

pid=$(daemon_pid)
if [ -n "$pid" ] && pid_alive "$pid"; then
  pass "daemon process $pid is alive"
  daemon_pids+=("$pid")
else
  fail "could not find a live daemon pid (got '$pid')"
fi

if wait_for 15 fetch_marker; then
  pass "HTTP request flows through the tunnel"
else
  fail "HTTP request through 127.0.0.1:$LOCAL_PORT failed"
  diagnose_forward
fi

# --- single-instance guard ---------------------------------------------------

if out=$("$TUNN" --detach smoke 2>&1); then
  fail "a second tunn --detach succeeded while the daemon was running: $out"
  second=$(daemon_pid)
  [ -n "$second" ] && daemon_pids+=("$second")
elif echo "$out" | grep -q "already running"; then
  pass "second tunn --detach is refused"
else
  fail "second tunn --detach failed for an unexpected reason: $out"
fi

# --- stop --------------------------------------------------------------------

out=$("$TUNN" stop 2>&1)
echo "$out"
if echo "$out" | grep -q "tunn daemon stopped"; then
  pass "tunn stop reports the daemon stopped"
else
  fail "tunn stop did not report the daemon stopped"
fi

if [ -n "$pid" ] && wait_for 10 daemon_dead "$pid"; then
  pass "daemon process exited"
else
  fail "daemon process $pid is still running after tunn stop"
fi

if wait_for 10 port_closed; then
  pass "forwarded port closed after stop"
else
  fail "127.0.0.1:$LOCAL_PORT still forwards traffic after tunn stop"
fi

if status_says "not running"; then
  pass "tunn status reports not running"
else
  fail "tunn status after stop: $("$TUNN" status 2>&1)"
fi

# Leave a clean slate for the next section whatever happened above.
for p in "${daemon_pids[@]:-}"; do [ -n "$p" ] && kill_tree "$p"; done
daemon_pids=()

# --- port conflict detection -------------------------------------------------

"$PY" -c "import socket, time
s = socket.socket()
s.bind(('127.0.0.1', $BUSY_PORT))
s.listen()
time.sleep(600)" &
bg_pids+=($!)
sleep 1

out=$("$TUNN" --detach conflict 2>&1)
echo "$out"
pid=$(daemon_pid)
[ -n "$pid" ] && daemon_pids+=("$pid")

if wait_for 10 status_says "is being used by"; then
  pass "port conflict on $BUSY_PORT is reported"
else
  fail "port conflict on $BUSY_PORT was not reported"
fi
"$TUNN" status 2>&1
"$TUNN" stop >/dev/null 2>&1

# --- summary -----------------------------------------------------------------

echo
if [ "$failures" -eq 0 ]; then
  echo "smoke test passed"
else
  echo "smoke test: $failures check(s) failed"
  exit 1
fi
