# Changelog

All notable changes to this add-on will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [0.4.1] - 2026-06-12

### Fixed
- Start-up failed with "No hubs configured" even when hubs were set in the UI. The run script now reads the `hubs` list directly from `/data/options.json` with `jq` instead of `bashio::config`, which does not reliably return complex list values. On a genuine empty config it now also logs which option keys are present (no secrets) to aid diagnosis.

## [0.4.0] - 2026-06-11

### Added
- **Multiple hub support**: configure any number of SmartDoor hubs/garage doors, each with its own host, share code, account password, and MQTT topic prefix. Configuration is now a repeatable **hubs** list in the add-on UI.
- Per-hub credentials are stored under `/config` keyed by each hub's stable **base station ID** (`dd-credentials-<bsid>.json`), so changing a hub's name or MQTT prefix no longer triggers re-registration.
- Automatic per-hub registration on startup: a hub with a share code but no stored credentials registers itself; hubs that already have credentials skip registration.

### Changed
- Registration moved from the one-time init script into the service, which now manages all hubs from a single process sharing one MQTT connection.
- The `code`, `password`, `host`, and `mqtt_prefix` top-level options are replaced by per-hub fields inside the new `hubs` list. **Existing single-hub users must move their settings into a single hub entry and will re-register on first start of this version.**

### Fixed
- Eliminated a latent race where the periodic status poller could send on a closed channel after the message loop ended; hub shutdown is now driven by context cancellation, and one hub losing its connection no longer affects the others.
- Registration now verifies the share code belongs to the hub at the configured host (matching base station IDs) before saving credentials, so a mismatched share code fails clearly instead of persisting bad credentials that would be reused on later starts.
- `mqtt_prefix` values are rejected if they contain `/`, `+`, or `#`, which would otherwise misroute commands or break MQTT subscriptions.
- The run script now fails fast (without leaving a partial file) if the `hubs` configuration cannot be parsed as JSON.
- First-time registration now requires a password as well as a share code, failing locally with a clear message instead of attempting to register with an empty password.
- A device whose ID is already owned by another hub is now refused with a loud error instead of silently driving the wrong hub's connection. Device registration is atomic, so two hubs starting at once cannot overwrite each other. (Assumes SmartDoor device IDs are unique across your hubs; if they collide, the conflicting door is reported and left uncontrolled rather than corrupted.)
- Fixed a data race on `Conn.pendingMessages` between the status poller and the message loop sharing one connection; reads/clears now use the same lock as the appends.
- The run script now validates that `hubs` is a non-empty list before starting, and writes the runtime hub file (which may contain share codes/passwords) with `0600` permissions.

## [0.3.3] - 2025-12-02

### Fixed
- Removed incorrect watchdog configuration that was causing add-on to restart every ~4 minutes
- Watchdog was checking MQTT broker TCP port instead of add-on health (add-on is a client, not a server)

## [0.3.2] - 2025-12-02

### Fixed
- Removed MQTT section from schema validation to fix "Missing option 'mqtt?'" error
- MQTT configuration is now truly optional - add manually only if using custom broker
- Schema validation no longer blocks add-on installation when MQTT section is omitted

## [0.3.1] - 2025-12-02

### Changed
- Made MQTT configuration completely optional - now defaults to Home Assistant MQTT integration
- Removed requirement to specify MQTT broker settings in configuration
- MQTT service availability check only runs when using HA MQTT (not custom brokers)

### Fixed
- AppArmor profile name now matches add-on slug (was causing loading errors)
- Removed nested AppArmor profile structure
- Simplified configuration schema for better user experience

## [0.3.0] - 2025-12-02

### Added
- P **Position Control**: Full slider support for setting door position (0-100%)
  - New MQTT topics: `position` and `set_position`
  - Real-time position reporting with 5% granularity
  - Support for common presets (Pet Mode 20%, Delivery Mode 68%, Ventilation 5-10%)
- MQTT prefix configuration option (`mqtt_prefix`)
- Custom MQTT broker support (optional, defaults to HA MQTT service)
- Watchdog monitoring on MQTT TCP port
- Comprehensive unit tests for position mapping
- Complete architecture documentation

### Changed
- Updated add-on name to "SmartDoor MQTT Bridge"
- Improved add-on description
- Enhanced MQTT configuration with nested schema
- Set `startup` to `application` and `boot` to `auto` per HA best practices
- Updated AppArmor profile with correct service names and network permissions
- Improved error handling throughout codebase

### Fixed
- Critical race condition in MQTT command handler
- Aggressive Fatal() calls replaced with graceful error handling
- Thread-safe device FSM map access with RWMutex
- Better error context in crypto operations
- Documentation gaps filled with comprehensive guides

### Documentation
- Added `POSITION_CONTROL.md` with usage examples
- Added `IMPROVEMENTS.md` summarizing all enhancements
- Updated `README.md` with complete architecture documentation
- Created `ICON_README.md` for visual assets guidance

## [0.2.0] - Previous Release

### Added
- Initial MQTT integration
- Basic FSM state management
- Multi-architecture support

### Fixed
- MQTT connection resilience improvements
- Device state tracking

## [0.1.9] - Initial Release

### Added
- SmartDoor device integration
- Home Assistant MQTT discovery
- Basic door control (open/close/stop)
