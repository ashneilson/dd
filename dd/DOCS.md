# SmartDoor MQTT Bridge Integration

This Home Assistant add-on bridges local SmartDoor garage door control systems with Home Assistant via MQTT, including support for precise position control, status monitoring, and prometheus metrics.

It supports **multiple hubs** — handy when you have more than one garage door, each with its own built-in hub, IP address, and share code.

---

## How to Use

### 1. Installation

1. Add this repository URL to your Home Assistant Add-on Store: `https://github.com/gravypower/dd`
2. Search for **SmartDoor MQTT Bridge** and click **Install**.

### 2. Configuration & One-Time Registration

To talk to each SmartDoor hub, the add-on needs cryptographic credentials which are acquired during a **one-time registration** process with the SmartDoor cloud servers. This happens automatically the first time the add-on starts, per hub.

In the add-on **Configuration** tab you will see a **SmartDoor Hubs** list. For **each** garage door / hub, add an entry with:

   - **name**: A friendly name for the hub (used as the Home Assistant device name, e.g. `Left Garage`).
   - **host**: The local IP address of that SmartDoor hub (e.g., `192.168.1.81`).
   - **mqtt_prefix**: A **unique** MQTT topic prefix for this hub (e.g. `dd-door-left`). Each hub must use a different prefix.
   - **code**: The **Registration Share Code** generated from your official SmartDoor mobile app for *that* hub. Only needed for the first registration.
   - **password**: Your SmartDoor account password. Only needed for the first registration.

Use **Add another** to configure a second (or third) hub.

Click **Save** and start the add-on. Check the **Log** tab — for each hub you should see either:

   - `Registered new hub credentials` (first run), or
   - `Hub credentials ready` followed by `Connected to hub` (subsequent runs).

> [!TIP]
> Credentials are stored per hub under `/config`, keyed by each hub's **base station ID** (a stable hardware identifier fetched from the hub itself). This means you can freely change a hub's `name` or `mqtt_prefix` later without triggering re-registration.

> [!TIP]
> After registration is successful, the generated credentials are securely saved locally. You can safely **delete the share code and password** from a hub's configuration if you wish.

---

## Configuration Settings

Each entry in the **hubs** list accepts:

| Setting | Type | Description |
| :--- | :--- | :--- |
| **name** | `string` | Friendly name for the hub; used as the Home Assistant device name. |
| **host** | `string` | The local IP address of this SmartDoor hardware unit. |
| **mqtt_prefix** | `string` | Unique topic prefix used for Home Assistant discovery and status for this hub. |
| **code** | `string` | One-time share code from your mobile app for this hub (only needed for first registration). |
| **password** | `password` | Your account password (only needed for first registration). |

Global settings (apply to all hubs):

| Setting | Type | Description |
| :--- | :--- | :--- |
| **poll_interval** | `integer` | How often to poll each physical door for status changes (default: `60` seconds). |
| **debug** | `boolean` | Set to `true` to print verbose API and cryptographic messages. |

---

## MQTT Topics & Discovery

This add-on supports Home Assistant MQTT Discovery automatically. Once started, a **Cover** entity is registered for every door discovered across all configured hubs.

If you are using custom MQTT scripts, the following topics are exposed per device (using a hub prefix of `dd-door` and the door's `<device_id>` as an example):

* **Config / Discovery**: `homeassistant/cover/<device_id>/config`
* **Command Topic**: `dd-door/<device_id>/command` (Accepts `GO_OPEN`, `GO_CLOSE`, `STOP`, `ONLINE`, `OFFLINE`)
* **Set Position Topic**: `dd-door/<device_id>/set_position` (Accepts a target position integer `0-100`)
* **State Topic**: `dd-door/<device_id>/state` (Reports `open`, `closed`, `opening`, `closing`, `stopping`)
* **Position Topic**: `dd-door/<device_id>/position` (Reports current door position `0-100`)
* **Availability Topic**: `dd-door/<device_id>/availability` (Reports `online` / `offline`)