# Changelog

All notable changes are documented here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.5.0] - 2026-09-28

### Changed
- Focused agent/UI on Docker status and controls, host hardware metrics, and
  explicitly selected systemd unit states. systemd reads the host D-Bus directly.
- Removed VPN collectors and bundled CLI, HTTP/TCP probes, public-IP/network
  inventory, image update checks and host/service actions from the active runtime.
- Old configuration remains readable but removed modules are never started.
- Removed virtual Docker/VPN interface noise; temperatures enabled for new configs.
- Reconcile obsolete retained discovery, including unselected service states.

## [0.4.1] - 2026-09-27

### Fixed
- Bundled Tailscale CLI in the image, automatic host socket detection through
  HOST_RUN, and an explicit socket-path setting for shared daemon sockets.
- Actionable Docker socket permission/mount errors and VPN setup guidance.

### Added
- First-run Linux setup script generating persistent configuration and Komodo
  Compose with detected Docker GID, host identity, random login credentials,
  and discovered Docker/Tailscale monitoring. Existing config is preserved.
- Container/setup smoke checks in CI.

## [0.4.0] - 2026-09-27

### Added
- Separate Home Assistant container and service devices prefixed with the server
  name, linked to the server, with Model/type metadata and matching control buttons.
- Container status sensors and type/server reading attributes.

### Changed
- Short entity names within child devices and one-decimal percentage display.
- Existing discovery topics and entity unique IDs remain unchanged on upgrade.

## [0.3.2] - 2026-09-27

### Fixed
- Docker stats use bounded parallel requests with individual timeouts instead of
  sharing an expiring inventory timeout across every container.

## [0.3.1] - 2026-09-27

### Fixed
- Native browser Save and Reload forms no longer lose their Origin header due to
  the referrer policy. Same-origin form posts work while null/foreign origins and
  invalid CSRF tokens remain rejected.

### Changed
- Replaced browser Basic Auth prompts with a login page, expiring server-side
  sessions and CSRF-protected sign out; existing web UI credentials still apply.
- Renamed the project to LabBeacon (`labbeacon` binary, service and container).
- Standardized the web UI, status/error messages, README and documentation examples on English.

## [0.3.0] - 2026-09-27

### Added
- Embedded HTTP settings UI with module/MQTT status refreshed every five seconds.
- Service editor, module switches and Docker control allow/deny inventory checklists.
- Validated atomic YAML persistence, download/export, manual reload and stale-form protection.
- Live configuration updates with explicit restart notices for MQTT, identity and UI settings.
- Loopback default, optional Basic Auth, CSRF tokens, origin/host checks and explicit
  acknowledgment for host-control/confirmation changes.
- Persistence, security and runtime integration tests for the web configuration flow.

### Changed
- Monitoring and HTTP startup remain available while MQTT reconnects.
- Unchanged ENV references/comments survive saves; password fields are never prefilled.
- Compose mounts a writable config directory for atomic saves; systemd grants a scoped
  writable config path. Default Compose no longer injects blank MQTT credential overrides.
- Literal dollar signs in YAML strings use `$$`.

## [0.2.0] - 2026-09-27

### Added
- Module registry with configurable collector adapters and an optional action provider interface.
- Explicitly opt-in host reboot/shutdown, systemd service start/stop/restart,
  Docker container start/stop/restart and supervisor-managed agent restart.
- Per-container allow/deny policy, read-only startup preflight and execution-time
  target validation; no host/systemd control inside containers.
- Structured action audit, bounded command queue, expiry/cooldown, per-connection
  session nonces and retained/duplicate command rejection.
- MQTT button discovery with stale-button cleanup and immediate refresh after actions.
- Confirmed-action guard with a Home Assistant dashboard/script example (MQTT
  discovery itself does not support confirmation dialogs).
- Agent version/uptime; optional CPU temperatures, interval-based top CPU/RAM
  processes, visible file descriptor totals, process count and boot timestamp.
- Container stats and restart counts; optional cached anonymous registry digest comparison.
- Optional interface IPs and cached public IP lookup.
- Tests for authorization gates, command routing, graceful agent restart, stats,
  registry updates and public IP caching.

### Changed
- Core collection uses the module registry; all remote control remains disabled by default.
- Docker socket access is used for POST operations only after explicit control opt-in.

## [0.1.0] - 2026-09-27

### Added
- System metrics, per-interface network throughput and multiple disk paths.
- Optional Docker Engine inventory with uptime and health attributes.
- HTTP/TCP service checks and optional Tailscale/NetBird CLI collectors.
- Home Assistant MQTT discovery, expiration, retained availability and LWT.
- YAML configuration, environment overrides, validation and structured logs.
- Graceful shutdown, reconnect backoff and module-level error isolation.
- Docker/Compose, systemd example, tests and multi-architecture GHCR workflow.
