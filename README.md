# LabBeacon

A small host agent for Home Assistant. Three things, with an embedded English
settings UI and MQTT discovery:

- **Docker:** each container gets its own device with status, restart count and
  optional CPU/RAM. Start, stop and restart buttons are explicitly enabled.
- **Host hardware:** CPU usage/model/cores, RAM, swap, disks, uptime, temperature
  where the kernel exposes sensors, and traffic on non-Docker/VPN interfaces.
- **Selected systemd services:** only exact `.service` units you list, reporting
  states such as `active`, `inactive`, `failed`, or `not-found`. Status only.

No Tailscale/NetBird, HTTP/TCP probes, public-IP lookups, registry update checks,
process inventory, host reboot/shutdown, or systemd control in the active runtime.

## Install with Docker / Komodo

On the Linux Docker host, download and review [deploy/setup.sh](deploy/setup.sh),
then run:

```sh
sudo sh setup.sh --lan /srv/appdata/labbeacon
sudo cat /srv/appdata/labbeacon/compose.generated.yaml
```

Paste the generated Compose into Komodo **UI Defined**, then **Save → Pull Images
→ Deploy**. The script prepares files only; it does not start containers. It:

- Detects the Docker socket group and generates its mount and `group_add`.
- Creates config owned by container UID 65532, with hostname-based identity.
- Prints a random login password for `admin` and enables host/Docker monitoring.
- Leaves all control actions and service monitoring opt-in.
- Preserves an existing config and refuses to overwrite generated Compose.

`--lan` explicitly allows LAN access. Omit it to bind to localhost and use
`ssh -L 8011:127.0.0.1:8011 user@server`. Open `http://HOST:8011`, sign in,
configure MQTT in Settings, save, and restart for MQTT connection changes.
The UI works even if the initial broker `tcp://127.0.0.1:1883` is unavailable.
For rootless Docker set `DOCKER_SOCKET` when running setup. Existing config and
its directory must both be writable by UID 65532 for atomic saves.

For manual deployment use [deploy/compose.ghcr.yaml](deploy/compose.ghcr.yaml).
Mount the entire config directory, not only `config.yaml`:

```yaml
services:
  labbeacon:
    image: ghcr.io/brendlij/labbeacon:0.5.0
    restart: unless-stopped
    network_mode: host
    pid: host
    uts: host
    read_only: true
    group_add: ["989"] # Replace with: stat -c '%g' /var/run/docker.sock
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    environment:
      HOST_PROC: /hostfs/proc
      HOST_SYS: /hostfs/sys
      HOST_ETC: /hostfs/etc
      HOST_VAR: /hostfs/var
      HOST_RUN: /hostfs/run
    volumes:
      - /srv/appdata/labbeacon:/etc/labbeacon
      - /:/hostfs:ro,rslave
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

Use [configs/config.example.yaml](configs/config.example.yaml) as a starting point.
The published image supports Linux AMD64 and ARM64. Docker Desktop reports its VM,
not Windows/macOS host hardware. Docker socket access is powerful even when the
mount is `:ro`; it does not restrict Docker API operations.

## Home Assistant

Set **Agent & MQTT → Name** to `ser5-01` for this layout:

| Device | Model | Contents |
| --- | --- | --- |
| `ser5-01` | Server | Hardware, host metrics, container totals, agent version |
| `ser5-01_paperlessngx-broker-1` | Container | Status and permitted Docker controls |
| `ser5-01_SSH` | Service | State of your selected `ssh.service` |

Children link to the server with `via_device`; readings include `type` and `server`
attributes. Percentages suggest one decimal place. Keep the **Agent ID** unchanged
on upgrade so existing entity identities are retained. Container entity IDs still
follow Docker IDs; recreated containers may leave old unavailable entities.

Enable **Allow container control** and keep **Control actions enabled** on to
publish buttons. The allow/deny list applies to exact container names. An empty
allowlist permits all containers; deny takes precedence. Commands are validated
against the current container ID/name before execution. Retained, duplicate,
stale-session and oversized commands are rejected; broker ACLs must restrict
command publishing to trusted Home Assistant clients.

## Selected systemd services

Enable **systemd status** in Settings and add only the units you want, for example:

```yaml
modules:
  services:
    enabled: true
    bus_socket: "" # Auto-detect the host system bus.
    checks:
      - name: SSH
        type: systemd
        systemd_unit: ssh.service
        timeout: 5s
```

The agent reads systemd's `LoadState`, `ActiveState`, and `SubState` through D-Bus.
It does not start, stop or restart units. In the host-monitoring Compose above it
uses `/hostfs/run/dbus/system_bus_socket`; native installs use
`/run/dbus/system_bus_socket`. A custom **System bus socket** can be set in the UI.
Read access must be permitted by the host bus policy. No systemctl executable or
privileged container is required. The host must actually run systemd.
An inaccessible bus reports an error, never a fabricated inactive state.
Removing a selected unit removes its MQTT discovery on subsequent polls.

## Web UI

> Screenshot placeholder: overview and focused settings for host, Docker and systemd.

The binary embeds HTML/CSS/vanilla JS; there is no frontend build step.
Overview refreshes every five seconds. Settings support validated atomic writes,
YAML export, manual reload, and live changes to polling/modules/selected services.
Identity, MQTT connection and listener/login changes require a restart, indicated
in the UI. Saves reject stale revisions instead of overwriting concurrent edits.

```yaml
webui:
  enabled: true
  bind_address: 127.0.0.1
  port: 8011
  username: admin
  password: "${WEBUI_PASSWORD}"
  allowed_hosts: []
```

Credentials enable a normal login page with HttpOnly/SameSite sessions, expiring
in 12 hours or on agent restart. Sign out invalidates the current session.
POST requests require CSRF tokens and same-origin checks. Password fields are not
prefilled; blank preserves the old value. YAML exports include stored secrets.
Environment references are preserved when unrelated settings change. Legacy
`AGENT_ID`, `AGENT_NAME`, `MQTT_BROKER`, `MQTT_USER`, `MQTT_PASSWORD`, `LOG_LEVEL`,
`POLL_INTERVAL` and `EXPIRE_AFTER` environment overrides remain supported.

Do not expose the UI unprotected to the internet. HTTP does not encrypt login
credentials. Prefer an SSH tunnel for remote access. DNS access requires an exact
`allowed_hosts` entry. Proxy TLS termination/Forwarded headers are not trusted by
the current origin checks.

## Upgrade to 0.5

Change the image tag to `0.5.0`, pull and redeploy. Existing YAML fields from 0.4
are accepted for compatibility, but removed features are not registered or run.
Old HTTP/TCP checks are ignored; choose systemd units explicitly in Settings.
Saving the service editor replaces the old checks with that explicit selection.
Temperatures are on for new configs; enable **CPU temperature** for an older
config that explicitly disabled them. Hardware without readable sensors cannot
report a temperature.

Within subsequent MQTT polls, the agent removes its retained VPN, public-IP,
IP inventory, image-update, process, virtual-interface and old service discovery,
and obsolete control buttons. Other agents and hardware/container entities are
untouched. An MQTT user needs subscribe/publish permission for its own discovery
topics to perform cleanup. Offline HA receives the cleaned retained topics when
it reconnects. User-customized HA names may remain.

## Native binary / development

```sh
go build -o bin/labbeacon ./cmd/labbeacon
./bin/labbeacon -config config.yaml -check-config
./bin/labbeacon -config config.yaml
```

Use `/` instead of `/hostfs` for native disk paths. See
[deploy/labbeacon.service](deploy/labbeacon.service) for systemd supervision.

`go test ./...` includes an embedded MQTT broker. Linux CI also tests against
Mosquitto, exercises systemd D-Bus against a mock service, runs the race detector,
validates generated Compose/config, and builds the container. Windows builds and
unit tests run separately; actual host systemd monitoring is Linux-only.
Tags `v*` publish the multi-architecture image to GHCR after release tests pass.
