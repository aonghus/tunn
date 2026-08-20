![GitHub License](https://img.shields.io/github/license/strandnerd/tunn) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/strandnerd/tunn/ci.yml) ![GitHub Release](https://img.shields.io/github/v/release/strandnerd/tunn) ![GitHub Issues or Pull Requests](https://img.shields.io/github/issues/strandnerd/tunn)



# tunn - SSH Tunnel Manager

`tunn` is a developer-friendly wrapper around OpenSSH that makes it easy to manage multiple SSH tunnels defined in a simple configuration file

<img width="1536" height="649" alt="tunn-gophers" src="https://github.com/user-attachments/assets/9b88aa87-721b-4577-b0c1-2cf61af4d160" />

## Features

- 🚀 **Simple Configuration**: Define all your tunnels in a single YAML file
- 🔧 **Selective Tunnels**: Run all tunnels or specific ones by name
- 🔌 **Multiple Ports**: Support for multiple port mappings per tunnel
- 🔐 **SSH Integration**: Leverages your existing SSH configuration
- ⚡ **Parallel Execution**: All tunnels run concurrently
- 🧩 **Daemon Mode**: Background service with status reporting via IPC
- 🧼 **Lean Go Module**: Depends only on `gopkg.in/yaml.v3`, keeping builds clean and portable
- 🔧 **Native SSH Sessions**: Spawns the system `ssh` binary for each mapping, so keys and config behave exactly like your shell
- 🎚️ **Per-Port Processes**: Launches one PID per port to pave the way for fine-grained lifecycle controls
- 🔁 **Auto-Reconnect**: If a tunnel drops unexpectedly, it's retried automatically with exponential backoff
- ♻️ **Live Reload**: `tunn reload` (or `SIGHUP`) re-reads `~/.tunnrc` and applies changes without disturbing unaffected tunnels



![Screencast from 2025-09-23 22-19-13 (online-video-cutter com)](https://github.com/user-attachments/assets/dbce86b1-c40c-47b9-a89c-6e188ad6e4ee)




## Installation

### Quick Install

```bash
curl -sSL https://raw.githubusercontent.com/strandnerd/tunn/main/scripts/install.sh | sh
```

### From Go Install

```bash
go install github.com/strandnerd/tunn@latest
```

### Build Locally

```bash
git clone https://github.com/strandnerd/tunn.git
cd tunn
go build -o tunn
sudo mv tunn /usr/local/bin/
```

## Configuration

Create a `~/.tunnrc` file in your home directory:

```yaml
tunnels:
  api:
    host: myserver          # SSH host from ~/.ssh/config
    ports:
      - 3000:3000           # local:remote port mapping
      - 4000:4001
    user: apiuser           # optional: SSH user
    identity_file: ~/.ssh/id_rsa  # optional: SSH key

  db:
    host: database
    ports:
      - 3306:3306           # MySQL
      - 5432:5432           # PostgreSQL
    user: dbadmin           # optional: overrides SSH config

  cache:
    host: cacheserver
    ports:
      - 6379:6379           # Redis

  proxy:
    host: xyz.ucd.ie
    dynamic_ports:
      - 1080                 # SOCKS proxy, like `ssh -D 1080 xyz.ucd.ie`
```

### Configuration Fields

- `tunnels`: Map of tunnel names
- `host`: SSH host alias from `~/.ssh/config`
- `ports`: List of port mappings in `local:remote` format
- `dynamic_ports` (optional): List of local ports for SOCKS dynamic application-level forwarding (SSH `-D`)
- `user` (optional): SSH username (overrides `~/.ssh/config`)
- `identity_file` (optional): Path to SSH private key

## Usage

### Run All Tunnels

```bash
tunn
```

### Run Specific Tunnels

```bash
# Single tunnel
tunn api

# Multiple tunnels
tunn api db

# All database-related tunnels
tunn db cache
```

### Run Tunnels in the Background

```bash
tunn --detach

# Or only specific tunnels
tunn --detach api db
```

The CLI respawns itself as a daemon, stores metadata under `$XDG_RUNTIME_DIR/tunn` (or `~/.cache/tunn` when the runtime dir is unavailable), and immediately returns control to the terminal.

### Check Daemon Status

```bash
tunn status
```

The status command contacts the daemon's Unix socket, reporting the PID, mode, and the latest port states for each managed tunnel. If no daemon is running, a friendly message is printed instead.

### Stop the Daemon

```bash
tunn stop
```

The stop command asks the daemon to shut down cleanly, waits for it to exit, and reports success.

### Reload the Config

```bash
tunn reload
```

Re-reads `~/.tunnrc` and applies the changes to a running daemon without disturbing unaffected tunnels: new tunnels are started, removed tunnels are stopped, and tunnels whose definition changed are restarted — tunnels left untouched in the config keep their existing connection. `tunn reload` respects whichever subset of tunnels the daemon was originally launched with (e.g. `tunn --detach api db` only reloads `api`/`db`).

The same reload logic runs when a running `tunn` process (foreground or daemon) receives `SIGHUP`:

```bash
kill -HUP $(cat "$XDG_RUNTIME_DIR/tunn/daemon.pid")
```

### Output Example

```
Tunnels Ready

[api]
    3000 ➜ 3000 [active]
    4000 ➜ 4001 [active]
[db]
    3306 ➜ 3306 [connecting]
    5432 ➜ 5432 [active]
```

### Reconnection

If a tunnel's SSH connection drops unexpectedly (network blip, server reboot, etc.), `tunn` automatically retries it with exponential backoff (starting at 1s, doubling up to a 30s cap, resetting once a connection goes active again). While retrying, the port's status shows `reconnecting in <duration>`. Stopping `tunn` (e.g. Ctrl+C) always takes priority and stops retries immediately.

## SSH Configuration

`tunn` uses your system's SSH configuration. Make sure your hosts are defined in `~/.ssh/config`:

```ssh
Host myserver
    HostName 192.168.1.100
    User myuser
    Port 22

Host database
    HostName db.example.com
    User dbuser
    IdentityFile ~/.ssh/db_key
```

## Requirements

- Go 1.21 or higher (for building)
- OpenSSH client (`ssh` command)
- Valid SSH configuration
- macOS and Linux are supported today; Windows support is planned but not available yet

## Daemon Runtime Files

While running in detached mode, `tunn` stores the following files in its runtime directory:

- `daemon.pid` – PID of the active daemon; used to prevent duplicate launches.
- `daemon.sock` – Unix domain socket for control commands (e.g., `tunn status`).
- `daemon.log` – Aggregated stdout/stderr from the daemon process.

The directory is created with `0700` permissions, and files are cleaned up automatically when the daemon exits or when stale state is detected on the next launch.
