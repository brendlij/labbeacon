# LabBeacon

**LabBeacon** is a small Go agent for each server: system metrics, Docker inventory and service checks
sent to Home Assistant over MQTT, with optional control buttons and an embedded
web UI as of 0.3.0. Each agent appears as a separate device through MQTT Discovery.
Linux is the primary target; the binary also builds on Windows.

**Requirements:** a reachable MQTT broker, the MQTT integration enabled in Home
Assistant, and Go 1.25+ or Docker Engine with Compose on the Linux host. The agent
does not install a broker or Home Assistant. The Compose example builds directly
from the cloned repository, so a published image is not required.

> Screenshot placeholder: Home Assistant device “Server 1” with grouped sensors.
> Screenshot placeholder: dashboard with CPU, RAM, network and service availability.

## Quick start with Docker (Linux)

On your Linux server, with Git and Docker Compose installed:

```sh
git clone https://github.com/brendlij/labbeacon.git
cd labbeacon
mkdir -p config-data
cp configs/config.docker.example.yaml config-data/config.yaml
# Set agent.id, agent.name and mqtt.broker before starting.
# Add MQTT credentials if your broker requires them.
nano config-data/config.yaml
# Atomic UI saves require directory write access for the container UID:
sudo chown -R 65532:65532 config-data
sudo chmod 700 config-data
sudo chmod 600 config-data/config.yaml
docker compose run --rm labbeacon -config /etc/labbeacon/config.yaml -check-config
docker compose up -d --build
docker compose logs -f
```

The device appears under **Settings → Devices & services → MQTT**. The first
network throughput measurement arrives in the second collection cycle. Repeat
with a different `agent.id` for each server. IDs must be unique across the broker
and remain stable; duplicate IDs cause disconnections and mixed device data.

Compose mounts the host at `/hostfs`, sets the gopsutil variables `HOST_PROC`,
`HOST_SYS`, `HOST_ETC`, `HOST_VAR` and `HOST_RUN`, and shares the network, PID and
UTS namespaces. Metrics therefore describe the Linux host. Add disks to
`disk_paths`, for example `/hostfs/mnt/data`. The bind mount uses `rslave` so host
mounts become visible when mount propagation is configured appropriately.
Docker Desktop reports its Linux VM, not the Windows/macOS host.

The container runs as UID/GID 65532 without Linux capabilities. This UID must be
able to read the config and, for UI saves, write both the file and its directory.
The host mounts grant read access to host data; use this container on trusted hosts.

### Enable Docker inventory

1. Set `modules.docker.enabled: true`.
2. Add `/var/run/docker.sock:/var/run/docker.sock:ro` as a YAML list entry under
   `volumes` in `compose.yaml`.
3. Enable `group_add: ["${DOCKER_GID}"]` and start:

```sh
export DOCKER_GID=$(stat -c %g /var/run/docker.sock)
docker compose up -d --build
```

For rootless Docker, adjust the socket path and group permissions. A `:ro` socket
mount **does not prevent Docker API writes**. The agent uses only GET requests by
default. `control_containers.enabled: true` enables POST requests for start/stop/restart,
subject to the control master switch. Socket access grants extensive host permissions.

## Deploy with Komodo or another stack manager

Use **UI Defined** in Komodo and paste [deploy/compose.ghcr.yaml](deploy/compose.ghcr.yaml)
as the stack's Compose file. It pulls `ghcr.io/brendlij/labbeacon:0.3.1` instead of
building from source. Save the stack configuration, then deploy it.

First prepare the configuration **on the selected Docker server**, not inside the
Komodo container:

```sh
sudo install -d -m 0700 /opt/labbeacon/config
sudo curl -fsSL https://raw.githubusercontent.com/brendlij/labbeacon/main/configs/config.docker.example.yaml -o /opt/labbeacon/config/config.yaml
sudo nano /opt/labbeacon/config/config.yaml
sudo chown -R 65532:65532 /opt/labbeacon/config
sudo chmod 700 /opt/labbeacon/config
sudo chmod 600 /opt/labbeacon/config/config.yaml
```

Set `agent.id`, `agent.name`, the MQTT broker URL and any required credentials.
The directory mount must be writable for atomic UI saves. Do not replace it with
a read-only single-file mount. The image supports Linux amd64 and arm64.

The default web UI remains bound to `127.0.0.1:8011` on the Docker host because the
container uses host networking. Access it through the SSH tunnel described below.
For explicit LAN access, configure `webui.bind_address`, username and password;
Login over HTTP is not encrypted. No Compose `ports` mapping is needed with
host networking. Docker monitoring requires the optional socket mount/group access.

A missing stack Compose file and a missing LabBeacon `config.yaml` are separate
problems: the stack manager needs the Compose definition, while the agent reads
`/opt/labbeacon/config/config.yaml` through its directory mount.

## Quick start with the binary

```sh
go build -trimpath -ldflags="-s -w -X github.com/brendlij/labbeacon/internal/version.Version=0.3.1" -o bin/labbeacon ./cmd/labbeacon
cp configs/config.example.yaml config.yaml
# Configure the broker, ID, paths and example service checks.
./bin/labbeacon -config config.yaml -check-config
./bin/labbeacon -config config.yaml
```

`-version` prints the build version. `SIGINT`/`SIGTERM` cancels ongoing checks,
announces `offline` on a best-effort basis and disconnects MQTT. System and HTTP/TCP
checks work on Windows; use disk paths such as `C:\`. Load average may be unavailable
there. The Docker collector supports Unix sockets, not Windows named pipes.
Container deployment and systemd support target Linux.

### systemd

A unit is provided in `deploy/labbeacon.service`. Installation after building:

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin labbeacon
sudo install -m 0755 bin/labbeacon /usr/local/bin/labbeacon
sudo install -d -m 0700 -o labbeacon -g labbeacon /etc/labbeacon
sudo install -m 0600 -o labbeacon -g labbeacon config.yaml /etc/labbeacon/config.yaml
sudo install -m 0644 deploy/labbeacon.service /etc/systemd/system/
```

Optionally set `MQTT_USER=...` and `MQTT_PASSWORD=...` in
`/etc/labbeacon/environment`; these overrides lock the corresponding UI fields.
Then run `sudo systemctl daemon-reload` and
`sudo systemctl enable --now labbeacon`. The service user needs access to the
Docker socket group for Docker monitoring. VPN CLI queries require access to the
respective local daemon sockets.

## Web UI

Open **http://127.0.0.1:8011/** after startup, even while MQTT is disconnected.
The overview refreshes module and MQTT status every five seconds. Green indicates
successful collection, red indicates an error, and disabled modules or modules
awaiting their first collection are gray. Individual failed service checks are
valid measurements: the module indicator reports collection health, not whether
every target is reachable. Settings include module switches, MQTT fields, an
HTTP/TCP service editor and Docker allow/deny checklists based on the latest
successful inventory.

> Screenshot placeholder: web UI with module indicators and MQTT connection status.
> Screenshot placeholder: settings with the service editor and Docker permissions.

```yaml
webui:
  enabled: true
  bind_address: 127.0.0.1
  port: 8011
  username: ""
  password: ""
  allowed_hosts: []
```

To explicitly allow LAN access, set `bind_address: 0.0.0.0` (or a specific LAN IP),
`username: admin` and `password: "${WEBUI_PASSWORD}"`. Username and password must
both be set or both be empty. The default bind address remains loopback even with
authentication enabled. To use DNS names, also set an exact hostname allowlist,
for example `allowed_hosts: [server.home.arpa]`. The UI rejects other hostnames to
protect against DNS rebinding.

When credentials are configured, `/login` displays a normal sign-in page using
the existing `webui.username` and `webui.password`. Sessions expire after 12 hours
or an agent restart; **Sign out** immediately invalidates the current session.
Session cookies are HttpOnly and SameSite=Strict (Secure on direct HTTPS).
Login, logout, Save and Reload all require CSRF tokens.

**Do not expose the UI to the internet without protection.** It provides access
to configuration, control permissions and stored secrets. Login over HTTP
does not provide encryption. For remote access, preferably keep loopback binding
and use an SSH tunnel: `ssh -L 8011:127.0.0.1:8011 user@server`, then open the local
URL. Reverse-proxy TLS termination is not currently configured: the UI does not
trust Forwarded headers and checks POST Origin against the direct connection.

`control_actions.enabled` is the master switch for all control actions. It defaults
to `true` for compatibility with 0.2.0; individual actions still require separate
opt-in and are disabled by default. Turning the master switch off preserves the
individual permissions but prevents all actions.

**Saving and reloading:** the poll interval, `expire_after`, service list, module
options and control permissions apply after the current collection/action cycle
without a restart. MQTT connection settings, agent ID/name, log level and web UI
listener/authentication settings require a restart. The UI explicitly lists these
differences; previous values remain active until restart. Reload reads the file
again but does not save unsent form changes. Invalid files leave the active
configuration untouched. Host control and `confirm_required` changes also require
the confirmation checkbox; saving does not itself reboot or shut down the host.

POST requests use signed CSRF tokens and SameSite cookies. File revisions detect
concurrent changes (409: reload the form). Password fields are never prefilled:
leaving them blank preserves the existing value; clearing requires an explicit
checkbox. Direct environment overrides are locked. Unchanged `${ENV}` references
survive saves; edited values are stored as literals. YAML exports contain
**stored plaintext secrets**, but do not resolve environment references. Protect
backups accordingly, and set referenced variables on any destination host.

**File permissions:** the agent writes a temporary file in the same directory,
syncs it and atomically replaces the config. The resulting Linux file mode is
`0600`. The service user needs write access to the config directory. Saves reject
symlinks as the config file. The example systemd unit grants a writable config
path at `/etc/labbeacon`. An externally managed file can remain read-only;
overview, export and reload still work, but UI saves will fail.

**Docker / upgrading from 0.2.0:** a single-file bind mount cannot be replaced
atomically. Compose therefore mounts the entire `config-data` directory writable;
the rest of the container remains read-only. Copy an existing `configs/config.yaml`
there and grant UID/GID 65532 write access (see quick start). Compose no longer
injects empty MQTT environment overrides. For old `${MQTT_USER}`/`${MQTT_PASSWORD}`
references, either explicitly pass these variables under `environment` or configure
the values in YAML. Explicit overrides remain locked in the UI.

Endpoints: `GET /`, `GET /config`, `POST /config`, `GET /config/export` and
`POST /config/reload`. HTML, CSS and vanilla JavaScript are embedded in the binary
with `go:embed`; no frontend build toolchain is required.

## Configuration

A YAML file is the configuration source. The web UI saves changes and applies
supported settings live; use “Reload without restarting” after external edits.
Unknown fields, multiple YAML documents, invalid IDs, unsupported check types and
missing required values are rejected at startup. Data modules are disabled by
code defaults except the agent's version/uptime sensors. Example configs enable
appropriate modules. All individual control features are disabled by default.

| Key | Default | Meaning |
| --- | --- | --- |
| `agent.id` | required | Letters, digits, `_`, `-`; stable unique ID |
| `agent.name` | required | Device display name |
| `agent.poll_interval` | `20s` | Collection interval, at least `1s` |
| `agent.expire_after` | `60s` | Whole seconds, greater than the poll interval |
| `agent.log_level` | `info` | `debug`, `info`, `warn`, `error`; JSON logs to stderr |
| `mqtt.broker` | required | URL, e.g. `tcp://host:1883` or `ssl://host:8883` |
| `mqtt.username` / `mqtt.password` | empty | Broker credentials |
| `mqtt.discovery_prefix` | `homeassistant` | Discovery prefix matching Home Assistant |
| `modules.system.enabled` | `false` | Collect system metrics |
| `modules.system.disk_paths` | `["/"]` | Disk paths in the agent's namespace |
| `modules.docker.enabled` | `false` | Query Docker Engine |
| `modules.docker.socket_path` | `/var/run/docker.sock` | Unix socket |
| `modules.docker.timeout` | `5s` | Time budget for the complete inventory cycle |
| `modules.services.enabled` | `false` | Enable service checks |
| `modules.services.checks` | `[]` | List of checks; fields below |
| `modules.tailscale.enabled` | `false` | Query Tailscale status |
| `modules.tailscale.command` | `tailscale` | CLI name or absolute binary path, no shell arguments |
| `modules.tailscale.timeout` | `5s` | CLI timeout |
| `modules.netbird.enabled` | `false` | Query NetBird status |
| `modules.netbird.command` | `netbird` | CLI name or absolute binary path |
| `modules.netbird.timeout` | `5s` | CLI timeout |

Service fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `name` | required | Unique name; part of the stable sensor ID |
| `type` | required | `http` or `tcp` |
| `url` | required for HTTP | Absolute HTTP(S) URL |
| `expected_status` | `200` | Expected HTTP status code, 100–599 |
| `host` | required for TCP | Hostname or IP; IPv6 without square brackets |
| `port` | required for TCP | 1–65535 |
| `timeout` | `5s` | Positive per-check timeout |

HTTP checks use GET, match the exact status code and do not follow redirects.
TLS certificates are verified. TCP checks test connection establishment, not the
application protocol. At most eight services are checked concurrently. ICMP/ping
is not implemented, so raw-socket permissions are unnecessary. Modules collect in
parallel; each cycle has at most `poll_interval` time. Increase the interval and
`expire_after` for many slow checks. Missing values expire in HA instead of being
replaced with fabricated zeros.

### Environment variables and secrets

`${VARIABLE}` and `$VARIABLE` are expanded in YAML string values **after** YAML
parsing. Passwords containing colons or newlines therefore cannot inject YAML
structure. Referencing an unset variable causes a startup error; explicitly empty
variables are allowed. Write literal dollar signs as `$$`; the UI escapes these
automatically. Direct environment overrides are used unchanged.

Direct overrides after loading:

| Environment variable | Config |
| --- | --- |
| `AGENT_ID`, `AGENT_NAME` | `agent.id`, `agent.name` |
| `POLL_INTERVAL`, `EXPIRE_AFTER` | Corresponding agent durations |
| `LOG_LEVEL` | `agent.log_level` |
| `MQTT_BROKER`, `MQTT_USER`, `MQTT_PASSWORD` | Corresponding MQTT fields |

**Compose** reads `.env`. For direct binary execution, set variables in the process
environment; the binary does not load `.env`. Real configs and `.env` are excluded
by `.gitignore` and `.dockerignore`.

TLS schemes are `ssl`, `tls` and `wss`; `tcp` and `ws` are also supported. TLS uses
at least version 1.2 and system CA certificates. Supply custom CAs through the
system trust store or `SSL_CERT_FILE` in the Linux container. mTLS and an option
to disable certificate verification are not implemented.

## Sensors and failure behavior

| Module | Sensors / attributes |
| --- | --- |
| System | CPU %, logical cores, CPU model; load 1/5/15; RAM/swap used, total, %; disk used, total, % per path; RX/TX in B/s per interface; uptime in seconds, hostname, OS, platform, kernel |
| Docker | `containers_running`, `containers_total`; running sensor with `running`, `total`, `summary` and complete `containers` array |
| Services | Connectivity binary sensor per name; `response_time_ms`, `checked_at` (UTC), `check_type`, HTTP `status_code` or error message |
| Tailscale | Local online status, IP, count of peers reported online; IP list and backend state as attributes; optional selected exit node and status |
| NetBird | Management/signal connection, local IP (possibly CIDR), connected peers; total peer count as an attribute |

Docker details include `id`, `name`, `status`, `image`, `started_at`,
`uptime_seconds` and optional `health`. Stopped/restarting containers have uptime
0; running and paused containers use the time since `StartedAt`. All containers
are queried, including stopped ones. A list or inspect error discards that Docker
cycle so incomplete inventory is not reported as complete. For large inventories,
consider the Docker timeout and broker payload limit.

Network throughput is the difference between two byte counters divided by actual
elapsed time. An interface's first observation only establishes a baseline;
counter resets yield 0 instead of an overflow. All reported interfaces, including
loopback and virtual interfaces, are collected.

Tailscale/NetBird require their CLI **and** access to a running local daemon. The
default image includes neither CLI. Run on the host or use a custom image with
the CLI and daemon socket. A missing executable appears as a module error on the
overview and logs a warning. Reload configuration or restart the agent after
installing it. CLI failures or invalid JSON let existing sensors expire; explicit
offline states produce `OFF`. “Online peers” refers to the VPN software's reported
state, not an additional active ping.

## MQTT and Home Assistant

```text
homeassistant/<component>/<AGENT_ID>/<sensor_key>/config   (QoS 1, retained)
<AGENT_ID>/<component>/<sensor_key>/state                  (QoS 1, not retained)
<AGENT_ID>/availability                                  (QoS 1, retained)
```

Every entity references the same device with `identifiers: [AGENT_ID]`, display
name, manufacturer `labbeacon` and build version. Sensor states are JSON:

```json
{"value": 42.5, "attributes": {}}
```

Discovery includes state/attribute templates and `expire_after` for **all** sensors.
Binary sensors use `ON`/`OFF`. Path, interface and service names become readable
slugs with a hash suffix to avoid collisions. MQTT sensor IDs are stable; renaming
a service creates a new entity.

The agent refreshes discovery every collection cycle, allowing broker/HA restarts
in any order. Measurements are intentionally not retained: replaying old data must
not restart `expire_after`. HA receives fresh data by the next successful collection
cycle. Discovery and availability remain retained.

Initial MQTT connections retry with backoff from 1 to 30 seconds. After connection
loss, the MQTT client reconnects with a maximum 30-second backoff. A successful
reconnect triggers collection. Old measurements are not buffered on disk during
broker outages. The agent sends `online` after connecting and before publishing
measurements, and `offline` during graceful shutdown. The broker publishes the
`offline` LWT after unexpected connection loss, subject to TCP detection/keepalive.
If only a collector fails, its values become unavailable through `expire_after`.

### Clean up removed sensors

Disabling a module or renaming a check does not automatically delete retained
sensor discovery. Previous entities become unavailable. To remove **one specific
sensor**, publish an empty retained message to its previous discovery topic:

```sh
mosquitto_pub -h BROKER -t homeassistant/sensor/srv-01/cpu_percent/config -r -n
```

Supply broker credentials if needed. Active sensors recreate their discovery.
Also clean up old discovery topics after changing the agent ID.

### Example dashboard

New entities receive suggested IDs such as `sensor.srv_01_cpu_percent`. Home
Assistant may preserve existing IDs or add suffixes for collisions; check the
actual entity list and adjust the card.

```yaml
type: entities
title: Server 1
entities:
  - entity: sensor.srv_01_cpu_percent
    name: CPU
  - entity: sensor.srv_01_ram_percent
    name: RAM
  - entity: sensor.srv_01_uptime
    name: Uptime
  - entity: sensor.srv_01_containers_running
    name: Running containers
```

For service, disk and network sensors, select the actual IDs with hash suffixes
from HA. Container details are attributes of `containers_running`.

## Development and releases

```sh
gofmt -w cmd internal
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/labbeacon-arm64 ./cmd/labbeacon
```

The race detector requires a supported C toolchain; on Windows without one, use
`go test ./...`. Unit tests cover config/secrets, counter rates, Docker API/inventory,
HTTP/TCP, CLI JSON and discovery. An embedded local MQTT test broker checks publishing,
retained discovery, LWT, reconnect and shutdown; it is not included in the agent
binary. Optionally run
`MQTT_TEST_BROKER=tcp://127.0.0.1:1883 go test ./internal/mqtt -run TestBrokerIntegration`
against an **isolated test broker without authentication**. This test uses the ID
`integration` and publishes discovery/availability to that broker.

CI runs builds, vet, tests with the race detector, a Mosquitto test and Linux cross
builds, plus Windows build/tests and a container build. Tags such as `v0.3.1`
trigger a multi-architecture build (`linux/amd64`, `linux/arm64`) and push to
`ghcr.io/<owner>/<repo>`. The image exists only after a successful workflow. Make
the GHCR package public if public access is intended.

```sh
git tag v0.3.1
git push origin v0.3.1
```

Architecture: `internal/module.Module` exposes `Name()`, `Enabled()` and
`Collect(ctx)`. `module.Registration` adapts collector packages to this interface;
the registry handles parallel collection and error isolation. A new package needs
a collector and a registration in `registered()`. Optional `control.Provider`
implementations return dynamic actions through `Actions(ctx)`. `control.Action`
exposes `ID()`, `Execute(ctx)` and `RequiresConfirm()`; `control.Entry` adds a name,
module, preflight and availability transition. The main loop serializes actions
and collection cycles. Docker uses mockable API interfaces, CLI modules use a
runner, and HTTP modules use replaceable clients.

## Updating a server

From the repository directory on the server:

```sh
git pull --ff-only
docker compose up -d --build
docker compose logs --tail=100 labbeacon
```

Keep a protected backup of `config-data/config.yaml` before upgrades. Configuration
is stored outside the image and survives container rebuilds.

## Available modules

Defaults below refer to code defaults; example configs enable system metrics.

| Module / feature | Config switch | Default | Required permissions / prerequisites |
| --- | --- | --- | --- |
| Basic system metrics | `modules.system.enabled` | disabled | Readable system information / host mounts in containers |
| CPU temperature | `modules.system.temperature` | disabled | Readable hwmon/thermal sensors, optional `HOST_SYS` |
| Top processes + process count | `modules.system.processes` | disabled | Read process data; inaccessible processes are indicated |
| Open FDs + process count | `modules.system.file_descriptors` | disabled | Read process FD information; Linux preferred |
| Boot timestamp | `modules.system.boot_time` | disabled | Host boot information |
| Docker inventory + restart count | `modules.docker.enabled` | disabled | Docker socket: GET `/containers/*` |
| Docker CPU/RAM | `modules.docker.stats` | disabled | Additional GET `/containers/*/stats` |
| Image update digest | `modules.docker.image_updates.enabled` | disabled | GET `/images/*`, HTTPS access to anonymous registry |
| HTTP/TCP checks | `modules.services.enabled` | disabled | Network access to targets |
| Tailscale / NetBird | Respective `enabled` | disabled | CLI and accessible local daemon |
| Network IP addresses | `modules.network.enabled` | disabled | Readable network interfaces |
| Local IPs | `modules.network.local_ips` | enabled within network module | Host networking for host IPs in containers |
| Public IP | `modules.network.public_ip.enabled` | disabled | Outbound HTTPS to configured endpoint |
| Agent version/uptime | `agent.metrics_enabled` | enabled | No additional permissions |

All system extensions also require `modules.system.enabled: true`. `top_n` limits
the `top_cpu` and `top_ram` attribute lists (default 5, allowed 1–100). There is one
summary sensor, not an entity per process. CPU rates use differences between two
process observations; the first CPU list is empty. 100% equals one logical core,
so processes using multiple cores may exceed it. Process start times identify PID
reuse. RAM is RSS in bytes. File descriptors are summed over readable processes;
`partial`, `observed_processes` and `total_processes` indicate limited visibility.
The agent does not escalate privileges for process metrics. `hidepid` and container
namespaces may restrict the visible process list.

`cpu_temperature` reports the highest detected CPU sensor temperature; individual
readings are attributes. On Linux, `/sys/class/thermal` or `HOST_SYS` is used as a
fallback. No value is fabricated when no CPU sensor is readable. `boot_time` is a
UTC timestamp with HA device class `timestamp`; `agent_uptime` independently counts
time since process startup.

Container stats extend inventory attributes and create CPU/RAM sensors per
container. CPU uses Docker CPU/system-time differences and online core count.
RAM subtracts `inactive_file` or `total_inactive_file`. No CPU value is published
without a valid pair of CPU observations. Stats are collected only for running
containers. `restart_count` comes from Docker Inspect and is Docker's own counter,
not a complete audit of manual stop/start operations. Stats can take about one
second per container; increase `docker.timeout`, `poll_interval` and `expire_after`
for large inventories. A stats error does not discard previously collected inventory.

Image updates are checked at most every `6h` by default with a `5s` timeout. Local
RepoDigests are compared with anonymously fetched HTTPS Schema-2/OCI manifests and,
where applicable, their platform descriptors. A local digest in a manifest list
means no update for that image. Authentication, bearer-token flows and credentials
are not used. Responses 401/403/404, untagged image IDs, digest-pinned references and
missing RepoDigests are skipped. This includes public registries requiring anonymous
bearer tokens, often Docker Hub/GHCR. The agent never pulls or updates images.

The network module publishes one sensor per interface with the address count and
complete CIDR list in `addresses`. Public IP defaults are endpoint
`https://api.ipify.org`, `interval: 15m`, `timeout: 5s`; intervals below one minute
are rejected. The endpoint must return one IP as plain text. The cache is reused
between polls; `checked_at` records the last successful fetch. After a failed refresh,
old values are no longer published and HA lets the sensor expire. The next attempt
occurs after the cache interval.

## Available control actions

| Action | Module / permission | Default | Required permissions |
| --- | --- | --- | --- |
| Host reboot | `host_control.enabled` + `reboot.enabled` | disabled | Native Linux/systemd host, root, system bus/manager and configured executable |
| Host shutdown | `host_control.enabled` + `shutdown.enabled` | disabled | Same as reboot |
| Container start/stop/restart | `modules.docker.control_containers.enabled` | disabled | Linux Docker socket with GET and appropriate POST permissions |
| Service start/stop/restart | Per-check `allow_control: true` + `systemd_unit` | disabled | Native Linux/systemd host, root, exact loaded `.service` unit |
| Agent restart | `agent_control.enabled` | disabled | Supervisor with restart-on-failure or Docker restart policy |

The default systemd unit runs as an unprivileged user and does not permit host or
service control. Administrators must explicitly adjust it for those features,
for example through a drop-in with `User=root` and `Group=root`. The agent invokes
neither sudo nor interactive Polkit prompts. Host/systemd control is disabled on
Windows and in detected containers; `pid: host` or a mounted socket alone does not
enable it. Docker container control works in the normal agent container when socket
access permits it.

Before registration, the agent performs read-only checks of executables, host/systemd
prerequisites, loaded units and socket reachability. Units/targets are checked again
before execution. Unavailable actions are logged. Docker authorization plugins or
changing system-bus policies may allow GET but later reject POST; a complete proof
of write access would itself require a mutation. Execution errors are logged with
structured details and no automatic retry. A timeout may mean an accepted action
is still running.

### Configure permissions

```yaml
host_control:
  enabled: false
  timeout: 15s
  reboot:
    enabled: false
    command: [systemctl, --no-ask-password, reboot]
    preflight: [systemctl, --no-ask-password, show, --property=Version, --value]
    confirm_required: true
  shutdown:
    enabled: false
    command: [systemctl, --no-ask-password, poweroff]
    preflight: [systemctl, --no-ask-password, show, --property=Version, --value]
    confirm_required: true
agent_control:
  enabled: false
```

`command` and `preflight` are argument lists without an implicit shell; `preflight`
must be a **read-only** check. Both come exclusively from trusted local configuration,
never from MQTT payloads. Example container permissions within an enabled Docker module:

```yaml
control_containers:
  enabled: true
  allow: [web, adguard]
  deny: [postgres]
```

An empty allowlist permits all names after enabling control; deny always takes
precedence. Names are exact Docker names without leading slashes, not glob patterns.
Buttons target immutable container IDs. A new instance with the same name receives
new buttons. A name change after discovery is rejected before execution until the
inventory is refreshed.

For a service check, add `systemd_unit: adguard.service` and `allow_control: true`.
The service name and unit are separate fields. Units cannot contain options, paths
or wildcards. Docker/service actions have a 30-second budget; host actions use
`host_control.timeout`. Docker stop/restart uses a 10-second grace period.
Successful actions immediately trigger a collection/discovery cycle.

Agent restart announces MQTT offline, disconnects and exits with **code 75**.
The provided systemd/Compose restart rules restart the agent and reload configuration.
Without a supervisor, it stays stopped. Invalid modified configuration prevents
startup; validate first with `-check-config`.

### MQTT commands and audit

Button discovery: `homeassistant/button/<ID>/<action_id>/config`.
Command topic: `<ID>/button/<action_id>/command`, QoS 0, **not retained**.
Buttons share the monitoring device. `expire_after` applies only to sensors;
button availability uses the shared availability topic.

```json
{"session":"current-value-from-discovery","confirm":false}
```

The agent rotates `session` on MQTT connection changes and after every started
action attempt, including execution failures. The current session appears in
discovery `payload_press`, the agent version sensor's `control_session` attribute,
and JSON at `<ID>/control/session`. It protects against stale messages; it is
**not a secret authentication credential**. Retained replays, DUP packets, invalid
JSON, payloads over 512 bytes, old sessions and commands for disabled actions are
rejected. The queue holds eight commands, which expire after 15 seconds waiting.
Identical actions have a 2-second cooldown. Commands and collection cycles are serialized.

Audit logs record time, action, topic, QoS, start and result/rejection reason.
MQTT 3.1.1 does not forward publisher identity to subscribers, so `actor` is reported
as unknown. Enforce authentication/authorization at the broker: grant write access
to `<ID>/button/+/command` only to trusted HA/admin clients, use TLS and use broker
logs to identify users. The agent needs write access to its state/discovery topics,
read access to its button discovery for cleanup and, when controlling actions,
read access to its command topics. Confirmation does not replace these ACLs.

Before host actions, the agent publishes and awaits acknowledgment of `rebooting`
or `shutting_down` on availability. Command failure restores `online`; successful
agent shutdown sends `offline`. HA's MQTT topic monitor shows these transitions;
an automatic HA logbook entry is not guaranteed.

Stale **button** discovery is cleaned up automatically, including after restarts
with control disabled, possibly on the next poll. Sensor discovery follows the
manual cleanup procedure above. Docker inventory errors temporarily remove Docker
buttons from discovery as a precaution.

### Confirm dangerous actions in Home Assistant

**MQTT Discovery has no `confirmation` field.** Confirmation dialogs belong to
[dashboard actions](https://www.home-assistant.io/dashboards/actions), not the
[MQTT button integration](https://www.home-assistant.io/integrations/button.mqtt/).
The agent therefore enforces `confirm_required: true`: the normal `button.press`
payload does not confirm anything and is rejected. Instead, use this HA script in
`scripts.yaml` (adjust IDs/entity names and keep `agent.metrics_enabled` enabled):

```yaml
homelab_srv01_reboot:
  alias: Restart Server 1
  mode: single
  sequence:
    - action: mqtt.publish
      data:
        topic: srv-01/button/host_reboot/command
        qos: 0
        retain: false
        payload: >-
          {{ {'session': state_attr('sensor.srv_01_agent_version', 'control_session'),
              'confirm': true} | to_json }}
```

Dashboard card with a native confirmation dialog:

```yaml
type: button
name: Restart Server 1
icon: mdi:restart
tap_action:
  action: perform-action
  perform_action: script.homelab_srv01_reboot
  confirmation:
    text: Really restart Server 1?
hold_action:
  action: none
```

Use `host_shutdown` similarly for shutdown. Confirmation applies to this dashboard
card; direct script/MQTT calls cannot prove a human click and must be protected
through HA permissions/broker ACLs. With `confirm_required: false`, the discovered
button works directly without a dialog. The agent never automatically includes
`confirm: true` in discovery.

### Protocol implementation references

- [Home Assistant MQTT Discovery](https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery)
- [MQTT Sensor](https://www.home-assistant.io/integrations/sensor.mqtt/)
- [MQTT Binary Sensor](https://www.home-assistant.io/integrations/binary_sensor.mqtt/)
- [gopsutil](https://github.com/shirou/gopsutil)
- [Paho MQTT Go](https://github.com/eclipse-paho/paho.mqtt.golang)
- [NetBird CLI](https://docs.netbird.io/get-started/cli)

MIT license; see [LICENSE](LICENSE).
