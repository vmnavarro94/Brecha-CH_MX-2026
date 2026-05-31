# API REST + WebSocket

El bot expone un servidor HTTP en `:8080` (en Docker: `:8088` mapeado) con dos contratos: REST para snapshots y mutaciones, WebSocket para streaming en tiempo real.

Contrato OpenAPI 3.0 completo: [`openapi.yaml`](../openapi.yaml).

## WebSocket — `/ws`

Conexión gorilla/websocket. Reconnect handshake estándar. Una vez conectado, el server pushea eventos JSON, uno por línea:

```json
{ "type": "<event-name>", "data": <payload> }
```

### Eventos disponibles

| Evento | Throttle | Payload | Descripción |
|--------|---------:|---------|-------------|
| `price_update` | 250 ms | `{ exchange, bid, ask }` | BBO actualizado. Throttled — solo se publica el último por exchange cada 250ms. |
| `price_snapshot` | 5 s | `{ <exchange>: { exchange, bid, ask } }` | Snapshot completo de todos los precios. Útil para boot del cliente. |
| `spread_stats` | 250 ms | `[{ Pair, Mean, Std, Samples }]` | Estado actual de los `SpreadModel`. |
| `opportunity` | inmediato | `Opportunity` completo | Una opp detectada + procesada (executed o skipped). |
| `trade_executed` | inmediato | `Trade` completo | Un trade simulado se ejecutó. Incluye `partial_fill`, `requested_volume`, `strategy`. |
| `pnl_update` | inmediato | `{ total_pnl, trade_count, win_rate }` | Resumen de P&L tras cada trade. |
| `circuit_breaker` | inmediato | `{ state }` | Transición del risk manager (`active` / `watching` / `paused`). |
| `latency_stats` | 250 ms | `{ p50_us, p99_us, samples, updates_per_sec }` | Latencia del engine. |
| `uptime_stats` | 250 ms | `{ <exchange>: pct }` | Uptime % por exchange en la sesión. |

### Throttling

`internal/server/ws.go:throttledTypes` define cuáles eventos colapsan al último valor cada 250ms. Los eventos críticos (opportunity, trade_executed, circuit_breaker, pnl_update) son inmediatos para no perder data importante.

### Reconnect del cliente

`web/src/hooks/useMarketSocket.ts` implementa backoff exponencial capeado a 30 segundos. Cuando reconecta, dispara un `fetchInitialState` que hace REST a `/api/status`, `/api/trades`, `/api/spreads` para recargar.

---

## REST endpoints

### `GET /api/status`

Estado top-level del sistema.

```json
{
  "circuit_breaker_state": "active",
  "exchange_count": 10,
  "trade_count": 48575
}
```

### `GET /api/health`

Health check con disponibilidad de L2 + latencia de parse por exchange.

```json
{
  "binance": {
    "last_update_at": "2026-05-30T22:00:00Z",
    "last_update_age_ms": 150,
    "fresh": true,
    "uptime_pct": 0.99,
    "has_l2": true,
    "parse_p50_us": 12.5,
    "parse_p99_us": 45.0
  },
  "kraken": { ... }
}
```

### `GET /api/pnl`

Resumen de P&L acumulado.

```json
{
  "total_pnl": "12190.17",
  "trade_count": 48575,
  "win_rate": 0.95
}
```

### `GET /api/pnl-by-pair`

P&L agregado por par de exchanges (buy → sell).

```json
{
  "pairs": [
    { "pair": "binance->okx",  "total_pnl": 1850.25, "trade_count": 4392, "win_rate": 0.96 },
    { "pair": "htx->cryptocom", "total_pnl": 890.10, "trade_count": 1284, "win_rate": 0.94 }
  ]
}
```

### `GET /api/pnl-by-strategy`

P&L agregado por strategy. **Filtra los trades legacy sin tag de strategy** (no aparece bucket "unknown").

```json
{
  "strategies": [
    { "strategy": "triangular", "total_pnl": 8312.21, "trade_count": 4392, "win_rate": 1.00, "total_volume": 4392000.0 },
    { "strategy": "spatial",    "total_pnl": 1234.50, "trade_count": 195,  "win_rate": 1.00, "total_volume": 19.5 },
    { "strategy": "funding",    "total_pnl": 670.96,  "trade_count": 984,  "win_rate": 1.00, "total_volume": 9840000.0 }
  ]
}
```

Ordenado por `total_pnl` desc.

### `GET /api/spreads`

Snapshot actual de todos los SpreadModels.

```json
[
  { "Pair": "binance-okx",  "Mean": 0.000056, "Std": 0.000036, "Samples": 500 },
  { "Pair": "binance-bybit", "Mean": -0.000003, "Std": 0.000020, "Samples": 500 }
]
```

### `GET /api/trades`

Historial completo de trades (paginado del lado del cliente).

```json
[
  {
    "ID": "uuid-1",
    "OpportunityID": "uuid-opp-1",
    "BuyExchange": "binance",
    "SellExchange": "okx",
    "BuyPrice": "74130.17",
    "SellPrice": "74135.30",
    "Volume": "0.01200000",
    "GrossProfit": "0.06156",
    "Fees": "0.0148",
    "NetProfit": "0.04676",
    "Slippage": "0.0",
    "ExecutedAt": "2026-05-30T22:00:00Z",
    "requested_volume": "0.012",
    "partial_fill": false,
    "strategy": "spatial"
  }
]
```

### `GET /api/opportunities?status=<status>`

Opportunities persistidas por status (default `executed`).

Valores válidos: `detected`, `executed`, `skipped`, `expired`.

```bash
curl localhost:8088/api/opportunities?status=skipped
```

### `GET /api/config`

Snapshot de la config actual.

```json
{
  "demo_mode": false,
  "min_net_profit_pct": 0.0015,
  "max_position_usdt": 1000,
  "staleness_threshold_ms": 2000,
  "execution_interval_ms": 100,
  "circuit_breaker_n": 5,
  "circuit_breaker_loss_pct": -0.005,
  "fees": {
    "binance": { "taker_fee": 0.001, "slippage_factor": 0.0002, "withdrawal_btc": 0.00020, "network_latency_bps": 1.0 },
    "kraken":  { ... },
    ...
  }
}
```

### `PATCH /api/config`

Actualiza la config en caliente. Shallow merge — los campos no enviados quedan como estaban.

```bash
curl -X PATCH localhost:8088/api/config \
  -H 'Content-Type: application/json' \
  -d '{ "min_net_profit_pct": 0.002, "max_position_usdt": 2000 }'
```

Para editar fees:

```bash
curl -X PATCH localhost:8088/api/config \
  -d '{
    "fees": {
      "binance": { "taker_fee": 0.0008 }
    }
  }'
```

Responde con el estado completo aplicado.

---

## Backtest endpoints

### `POST /api/backtest/recording`

Activa/desactiva el recorder WAL.

```bash
curl -X POST localhost:8088/api/backtest/recording \
  -d '{"enabled": true}'
```

```json
{ "recording": true }
```

### `POST /api/backtest/start`

Lanza un replay sobre el rango grabado.

```json
{
  "from": "2026-05-30T21:00:00Z",
  "to":   "2026-05-30T22:00:00Z",
  "speed": 0,
  "strategies": ["spatial", "funding"],
  "seed": 42
}
```

Respuesta:

```json
{ "run_id": "31f12c00-bd46-4610-b429-a9404f3ce57d" }
```

`speed=0` significa "max" (réplica instantánea sin sleep). `1.0` = realtime, `2.0` = 2× realtime.

### `GET /api/backtest/status`

Estado del run actual.

```json
{
  "state": "running",
  "progress": 0.42,
  "current_ts": 1735598400000,
  "run_id": "31f12c00..."
}
```

`state ∈ { idle, running, done }`. `progress` va de 0 a 1.

### `GET /api/backtest/runs`

Historial de todos los runs persistidos.

```json
{
  "runs": [
    {
      "ID": "uuid",
      "StartedAt": "...",
      "EndedAt": "...",
      "FromTS": "...",
      "ToTS": "...",
      "StrategiesJSON": "[\"spatial\",\"funding\"]",
      "MetricsJSON": "{...}",
      "Status": "done"
    }
  ]
}
```

### `GET /api/backtest/results/{id}`

Métricas detalladas de un run específico.

```json
{
  "run_id": "uuid",
  "started_at": "...",
  "ended_at": "...",
  "from": "...",
  "to": "...",
  "status": "done",
  "metrics": {
    "spatial": {
      "TotalPnL": 145.23,
      "Sharpe": 2.1,
      "MaxDrawdown": 12.5,
      "HitRate": 0.94,
      "ProfitFactor": 3.2,
      "TradeCount": 89
    },
    "funding": { ... }
  }
}
```

---

## CORS

`ALLOWED_ORIGIN=*` por default. En producción se ajusta a un origin específico.

`PATCH` requiere headers:
```
Content-Type: application/json
```

`OPTIONS` preflight habilitado para todos los endpoints mutating.

---

## Codes de error

| Code | Cuándo |
|------|--------|
| `200` | OK. |
| `400` | JSON malformado o campo requerido faltante en PATCH. |
| `404` | Recurso no encontrado (ej. backtest run_id inválido). |
| `405` | Método HTTP no permitido. |
| `409` | Conflict (ej. iniciar backtest cuando ya hay uno corriendo). |
| `503` | Service unavailable (ej. recording endpoint cuando recorder no está configurado). |

Las respuestas de error son JSON:

```json
{ "error": "descripción del error" }
```
