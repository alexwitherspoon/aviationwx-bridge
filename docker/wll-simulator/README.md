# WeatherLink Live Local API simulator

Serves `GET /v1/current_conditions` shaped like the
[WeatherLink Live Local API](https://weatherlink.github.io/weatherlink-live-local-api/).

Each response uses a live Unix `ts` and lightly mutates ISS wind/temp so successive
bridge polls produce a visible series of weather POSTs.

## Run with bridge (recommended)

From the repo root:

```bash
docker compose -f docker/docker-compose.yml -f docker/docker-compose.weather-dev.yml up -d --build
```

| Service | URL |
|---------|-----|
| Console | http://localhost:1229 |
| WLL mock | http://localhost:18080/v1/current_conditions |
| Captured weather POSTs | https://localhost:18443/v1/bridge/weather/captured |

### Configure the bridge console

1. **Settings → AviationWX Link**
   - Enable, paste key: `awxb_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL`
   - Advanced base URL: `https://bridge-api-mock:8443`
   - Confirm (bootstrap). The mock uses a self-signed cert; the compose overlay sets `AVIATIONWX_API_TLS_INSECURE=1` (local only).
2. **Weather → Add Station**
   - Host: `wll-simulator:8080` (reachable from the bridge container)
   - Test poll → pick txid → Save / enable

Watch:

- Weather page raw payload log (LAN side)
- `curl -sk https://localhost:18443/v1/bridge/weather/captured | jq .` (exact outbound posts)

## Golden wire sample (no Docker)

The precise `POST /v1/bridge/weather` JSON (raw-only, no `sample`) from the Davis fixture:

[`internal/bridgeapi/testdata/weather_post_davis_wll.example.json`](../../internal/bridgeapi/testdata/weather_post_davis_wll.example.json)

Regenerate / assert:

```bash
UPDATE_GOLDEN=1 go test ./internal/station/ -run TestWeatherRequestGoldenDavisWLL
go test ./internal/station/ -run TestWeatherRequestGoldenDavisWLL
```

Share that file with core/frontend reviewers as the contract sample.

## Run simulator alone

```bash
go run ./docker/wll-simulator -addr :8080
curl -s http://127.0.0.1:8080/v1/current_conditions | jq .
```
