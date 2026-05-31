# SmartDoor MQTT Bridge Integration

This Home Assistant add-on bridges local SmartDoor garage door control systems with Home Assistant via MQTT, including support for precise position control, status monitoring, and prometheus metrics.

---

## How to Use

### 1. Installation

1. Add this repository URL to your Home Assistant Add-on Store: `https://github.com/gravypower/dd`
2. Search for **SmartDoor MQTT Bridge** and click **Install**.

### 2. Configuration & One-Time Registration

To talk to the SmartDoor device, the add-on needs cryptographic credentials which are acquired during a **one-time registration** process with the SmartDoor cloud servers.

1. Generate a **Registration Share Code** from your official SmartDoor mobile app.
2. In the add-on **Configuration** tab, enter:
   - **Registration Share Code**: The code you just generated.
   - **User Password**: Your SmartDoor account password.
   - **SmartDoor Device IP/Host**: The local IP address of your SmartDoor garage door (e.g., `192.168.1.81`).
3. Click **Save** and start the add-on.
4. Check the **Log** tab. You should see a message:
   `Registration successful. Credentials saved to /config/dd-credentials.json.`

> [!TIP]
> After registration is successful, the generated credentials are secure and saved locally. You can safely **delete the share code and password** from your add-on configuration tab if you wish.

---

## Configuration Settings

| Setting | Type | Description |
| :--- | :--- | :--- |
| **Registration Share Code** | `string` | One-time share code from your mobile app (only needed for first run). |
| **User Password** | `password` | Your account password (only needed for first run). |
| **SmartDoor Device IP/Host** | `string` | The local IP address of the SmartDoor hardware unit. |
| **MQTT Topic Prefix** | `string` | Topic prefix used for Home Assistant discovery and status (default: `dd-door`). |
| **Status Poll Interval (seconds)** | `integer` | How often to poll the physical door for status changes (default: `60` seconds). |
| **Enable Debug Logging** | `boolean` | Set to `true` to print verbose API and cryptographic messages. |

---

## MQTT Topics & Discovery

This add-on supports Home Assistant MQTT Discovery automatically. Once started, a new **Cover** entity will be registered in Home Assistant.

If you are using custom MQTT scripts, the following topics are exposed (using the default `dd-door` prefix):

* **Config / Discovery**: `homeassistant/cover/dd-door/config`
* **Command Topic**: `dd-door/set` (Accepts `OPEN`, `CLOSE`, `STOP`, or a target position integer `0-100`)
* **State Topic**: `dd-door/state` (Reports `open`, `closed`, `opening`, `closing`, or `stopped`)
* **Position Topic**: `dd-door/position` (Reports current door position `0-100`)