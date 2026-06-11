# Home Assistant Add-on: SmartDoor MQTT Bridge

[![Supports aarch64 Architecture][aarch64-shield]][addon-store]
[![Supports amd64 Architecture][amd64-shield]][addon-store]
[![Supports armhf Architecture][armhf-shield]][addon-store]
[![Supports armv7 Architecture][armv7-shield]][addon-store]
[![Supports i386 Architecture][i386-shield]][addon-store]

A secure and resilient integration that bridges **SmartDoor** smart garage door systems with Home Assistant via MQTT.

## Features

- **Multiple Hub Support**: Manage several SmartDoor garage doors/hubs from one add-on, each with its own host, share code, and MQTT topic prefix.
- **MQTT Discovery**: Automatic entity registration under Home Assistant (Cover entity) for every door across all hubs.
- **Position Control**: Full support for opening/closing to precise percentages (5% - 95%) or standard Open/Close/Stop commands.
- **At-Rest Encrypted Credentials**: Encrypts each hub's login credentials locally under `/config/dd-credentials-<bsid>.json` via AES-256-GCM.
- **Metrics Integration**: Built-in optional Prometheus metrics endpoint.
- **Robust FSM & Reconnection**: Handles network dropouts gracefully and recovers without crashing.
- **AppArmor Protection**: Strict kernel-level security isolation profile enforced by default.

## Installation & Setup

For step-by-step setup and configuration guidance, please see the [Add-on Documentation](DOCS.md).

[addon-store]: https://github.com/ashneilson/dd
[aarch64-shield]: https://img.shields.io/badge/aarch64-yes-green.svg
[amd64-shield]: https://img.shields.io/badge/amd64-yes-green.svg
[armhf-shield]: https://img.shields.io/badge/armhf-yes-green.svg
[armv7-shield]: https://img.shields.io/badge/armv7-yes-green.svg
[i386-shield]: https://img.shields.io/badge/i386-yes-green.svg
