#!/bin/sh
# Prepare files only. Does not start containers or modify existing config.
set -eu
umask 077
bind=127.0.0.1
if [ "${1:-}" = "--lan" ]; then bind=0.0.0.0; shift; fi
directory=${1:-/srv/appdata/labbeacon}
if [ "$#" -gt 1 ]; then echo 'Usage: setup.sh [--lan] [/absolute/appdata/path]' >&2; exit 1; fi
case "$directory" in /*) ;; *) echo 'Use an absolute appdata path.' >&2; exit 1;; esac
case "$directory" in *[!a-zA-Z0-9_./-]*) echo 'Use a path without spaces or special characters.' >&2; exit 1;; esac
if [ "$(id -u)" != 0 ]; then echo 'Run with sudo so new config files can be owned by container UID 65532.' >&2; exit 1; fi
socket=${DOCKER_SOCKET:-/var/run/docker.sock}
case "$socket" in /*) ;; *) echo 'DOCKER_SOCKET must be an absolute path.' >&2; exit 1;; esac
case "$socket" in *[!a-zA-Z0-9_./-]*) echo 'Use a Docker socket path without spaces or special characters.' >&2; exit 1;; esac
docker_enabled=false
gid=
if [ -S "$socket" ]; then
  docker_enabled=true
  gid=$(stat -c '%g' "$socket")
fi
tailscale_enabled=false
if [ -S /run/tailscale/tailscaled.sock ]; then tailscale_enabled=true; fi
if [ ! -d "$directory" ]; then
  mkdir -p "$directory"
  chown 65532:65532 "$directory"
  chmod 700 "$directory"
fi
compose="$directory/compose.generated.yaml"
if [ -e "$compose" ]; then echo "$compose already exists; keep it or move it before rerunning. Nothing overwritten." >&2; exit 1; fi
config_tmp=
compose_tmp=
trap '[ -z "$config_tmp" ] || rm -f "$config_tmp"; [ -z "$compose_tmp" ] || rm -f "$compose_tmp"' EXIT HUP INT TERM
if [ ! -e "$directory/config.yaml" ]; then
  chown 65532:65532 "$directory"
  chmod 700 "$directory"
  server=$(hostname | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_-')
  [ -n "$server" ] || server=labbeacon-server
  password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
  config_tmp=$(mktemp "$directory/.config.XXXXXX")
  cat > "$config_tmp" <<EOF
agent:
  id: "$server"
  name: "$server"
  poll_interval: 20s
  expire_after: 60s
mqtt:
  broker: tcp://127.0.0.1:1883
modules:
  system:
    enabled: true
    disk_paths: [/hostfs]
  docker:
    enabled: $docker_enabled
    stats: true
  tailscale:
    enabled: $tailscale_enabled
  services:
    enabled: false
    checks: []
webui:
  enabled: true
  bind_address: $bind
  port: 8011
  username: admin
  password: "$password"
EOF
  chown 65532:65532 "$config_tmp"
  chmod 600 "$config_tmp"
  mv "$config_tmp" "$directory/config.yaml"
  config_tmp=
  printf 'Created config. Login username: admin\nLogin password: %s\n' "$password"
else
  echo 'Existing config preserved, including credentials and module settings.'
fi
compose_tmp=$(mktemp "$directory/.compose.XXXXXX")
cat > "$compose_tmp" <<EOF
services:
  labbeacon:
    image: ghcr.io/brendlij/labbeacon:0.4.1
    restart: unless-stopped
    network_mode: host
    pid: host
    uts: host
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    stop_grace_period: 10s
EOF
if [ -n "$gid" ]; then printf '    group_add: ["%s"]\n' "$gid" >> "$compose_tmp"; fi
cat >> "$compose_tmp" <<EOF
    environment:
      HOST_PROC: /hostfs/proc
      HOST_SYS: /hostfs/sys
      HOST_ETC: /hostfs/etc
      HOST_VAR: /hostfs/var
      HOST_RUN: /hostfs/run
    volumes:
      - type: bind
        source: $directory
        target: /etc/labbeacon
        bind:
          create_host_path: false
      - type: bind
        source: /
        target: /hostfs
        read_only: true
        bind:
          propagation: rslave
EOF
if [ -n "$gid" ]; then
cat >> "$compose_tmp" <<EOF
      - type: bind
        source: $socket
        target: /var/run/docker.sock
        read_only: true
        bind:
          create_host_path: false
EOF
fi
mv "$compose_tmp" "$compose"
compose_tmp=
printf '\nCompose ready: %s\nPaste it into Komodo UI Defined, then Save, Pull Images, Deploy.\n' "$compose"
echo 'Set MQTT broker/credentials and service checks in the LabBeacon settings page.'
echo 'New configs keep controls disabled. Docker socket access itself grants powerful host permissions.'
echo 'Existing configs must allow UID 65532 to write both the config and its directory.'
echo 'Without --lan, use: ssh -L 8011:127.0.0.1:8011 user@server'
