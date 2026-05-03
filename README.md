# planespotter
Monitors a local instance of dump1090 / ultrafeeder and posts when planes are seen for the first time

## Configuration

Configuration is loaded from `.env` and `PLANESPOTTER_` environment variables.

- `PLANESPOTTER_TAR1090_URL`: base URL for the tar1090 instance.
- `PLANESPOTTER_DISCORD_WEBHOOK_URL`: Discord webhook URL to post new aircraft messages to.
- `PLANESPOTTER_DISCORD_WEBHOOK_THREAD_ID`: optional Discord thread ID for webhook messages.
- `PLANESPOTTER_MONITOR_INTERVAL`: polling interval, default `15s`.
- `PLANESPOTTER_MAX_ALTITUDE`: maximum altitude in feet. Aircraft above this altitude are ignored and are not recorded as seen. Default `10000`. Set to `0` or lower to disable.
- `PLANESPOTTER_SEEN_AIRCRAFT_PATH`: path to persisted seen-aircraft state, default `seen.json`.
- `PLANESPOTTER_LOG_LEVEL`: log level, default `INFO`.
