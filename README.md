# planespotter
Monitors a local instance of dump1090 / ultrafeeder and posts when planes are seen for the first time

## Configuration

Configuration is loaded from `.env` and `PLANESPOTTER_` environment variables.

- `PLANESPOTTER_TAR1090_URL`: base URL for the tar1090 instance.
- `PLANESPOTTER_DISCORD_WEBHOOK_URL`: Discord webhook URL to post new aircraft messages to.
- `PLANESPOTTER_DISCORD_WEBHOOK_THREAD_ID`: optional Discord thread ID for webhook messages.
- `PLANESPOTTER_MONITOR_INTERVAL`: polling interval, default `15s`.
- `PLANESPOTTER_MAX_ALTITUDE`: maximum altitude in feet. Aircraft above this altitude are ignored and are not recorded as seen. Default `10000`. Aircraft without a reported altitude are also ignored while this filter is enabled. Set to `0` or lower to disable.
- `PLANESPOTTER_CALLSIGN_WAIT_RECEIVES`: number of times to receive a newly detected aircraft without a callsign before posting anyway, default `4`. Set to `0` or lower to post immediately.
- `PLANESPOTTER_DATA_PATH`: directory for persisted runtime data, default `.`. Seen aircraft are stored in `seen.json` in this directory.
- `PLANESPOTTER_CCAR_ENABLED`: whether to enrich Canadian aircraft with the Canadian Civil Aircraft Registry. Default `true`. The CCAR database is cached in `ccarcsdb` in the data directory and refreshed every 14 days.
- `PLANESPOTTER_LOG_LEVEL`: log level, default `INFO`.
