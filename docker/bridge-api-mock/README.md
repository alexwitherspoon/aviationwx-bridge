# Bridge API mock (local weather capture)

HTTPS stub of `https://api.aviationwx.org/v1/bridge/*` for local wire review.

- `GET /v1/bridge/bootstrap`
- `POST /v1/bridge/health` → 204
- `POST /v1/bridge/weather` → 204, body stored
- `GET /v1/bridge/weather/captured` → newest-first array of received JSON bodies
- `POST /v1/bridge/weather/fail` with `{"count":N}` → next N weather POSTs return 503 (omit/`count`<=0 = fail until cleared)
- `DELETE /v1/bridge/weather/fail` → accept weather again
- `POST /v1/bridge/weather/captured/clear` → empty capture buffer

Uses an ephemeral self-signed certificate. Pair with `AVIATIONWX_API_TLS_INSECURE=1`
on the bridge (see `docker-compose.weather-dev.yml`).

```bash
go run ./docker/bridge-api-mock -addr :8443
```
