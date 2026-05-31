<p align="center">
  <img src="docs/img/brecha-logo.svg" alt="Brecha — Arbitrage Engine" width="520">
</p>

<p align="center">
  <strong>Motor de arbitraje de Bitcoin en tiempo real sobre 10 exchanges</strong><br/>
  Scoring estadístico · sizing Kelly · backtest determinístico
</p>

<p align="center">
  <a href="https://www.coding-challenge-mexico.com/challenge">
    <img alt="Coding Challenge Mexico 2026" src="https://img.shields.io/badge/Coding%20Challenge-Mexico%202026-F7931A?style=flat-square">
  </a>
  <img alt="Go 1.22" src="https://img.shields.io/badge/Go-1.22-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="React 18" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=black">
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-WAL-003B57?style=flat-square&logo=sqlite&logoColor=white">
  <img alt="License MIT" src="https://img.shields.io/badge/license-MIT-green?style=flat-square">
</p>

---

Detección de arbitraje de Bitcoin en tiempo real sobre 10 exchanges, con latencia de motor sub-milisegundo, modelo estadístico de reversión a la media que prioriza spreads anómalos, profundidad real/sintética de order book con ejecución walk-the-book y fills parciales, sizing Kelly por par y backtesting determinístico.

<p align="center">
  <img src="docs/img/00-dashboard-full.png" alt="Brecha dashboard" width="900">
</p>

## Documentación

Toda la documentación detallada vive en [`docs/`](docs/). Este README es el índice + cómo arrancar el proyecto.

| Documento | Contenido |
|-----------|-----------|
| [`docs/arquitectura.md`](docs/arquitectura.md) | Diagrama del sistema, capas, multi-strategy engine, processing loop, recorder. |
| [`docs/estrategias.md`](docs/estrategias.md) | Las tres strategies (spatial, funding, triangular) en detalle con diagramas de flujo y modelo de costos. |
| [`docs/scoring.md`](docs/scoring.md) | Z-score con SpreadModel Welford, fórmula de score normalizada, Kelly sizing, correlation penalty, adaptive threshold. |
| [`docs/dashboard.md`](docs/dashboard.md) | Tour panel por panel del dashboard. Cada estado, color y valor explicado. |
| [`docs/configuracion.md`](docs/configuracion.md) | Tabla completa de env vars con defaults y notas de tuning. |
| [`docs/api.md`](docs/api.md) | Contrato REST + eventos WebSocket. |
| [`docs/backtest.md`](docs/backtest.md) | Recorder WAL + replay determinístico. Métricas computadas. |
| [`docs/exchanges.md`](docs/exchanges.md) | Los 10 connectors WS, fees retail reales, disponibilidad de L2. |

## Quick start (Docker)

```bash
docker compose up --build -d
```

Dashboard en `http://localhost:8088`. Los 10 connectors arrancan en paralelo. Los spread models calientan en ~30 segundos (a 30 muestras). El priority queue empieza a dequeuear oportunidades cuando se cumple el threshold.

Verificaciones rápidas:

```bash
curl -s localhost:8088/api/status            | jq
curl -s localhost:8088/api/pnl                | jq
curl -s localhost:8088/api/health             | jq
curl -s localhost:8088/api/pnl-by-pair        | jq
curl -s localhost:8088/api/pnl-by-strategy    | jq
```

## Correr local sin Docker

```bash
# Backend
cp .env.example .env
go run ./cmd/server
# En otra terminal
cd web && npm install && npm run dev
```

Backend en `:8080`, frontend dev en `:5173`. Vite proxea `/api` y `/ws` al backend.

## Deploy

El proyecto se despliega con un único binario Go + assets estáticos servidos por el mismo proceso. Tres modos:

### Docker (recomendado)

`docker compose up --build -d` arma una imagen multi-stage (Go builder + Vite builder + runtime Alpine) y la corre con un volumen persistente para SQLite en `/data`. Ver `Dockerfile` y `docker-compose.yml`.

Tamaño final: ~15 MB sin CGO (gracias a `modernc.org/sqlite`).

### Binario standalone

```bash
cd web && npm install && npm run build
cd .. && go build -o server ./cmd/server
DATA_DIR=./data ./server
```

El binario embebe los assets de `web/dist/` y sirve el SPA en `/` + API en `/api/*` + WebSocket en `/ws`.

### Variables de entorno

Defaults razonables para arrancar sin tocar nada. Para tuning, ver [`docs/configuracion.md`](docs/configuracion.md).

## Tech stack

| Capa | Stack |
|------|-------|
| Backend | Go 1.22, `gorilla/websocket`, `shopspring/decimal`, `modernc.org/sqlite` (sin CGO) |
| Frontend | React 18, TypeScript, Vite, Zustand, Lucide icons |
| Storage | SQLite (un archivo, persistido entre restarts) |
| Build | Docker multi-stage (binario Go estático + bundle Vite) |

Sin wrappers HTTP, sin libs de state más allá de Zustand, sin histogramas. Stdlib + los cuatro paquetes arriba.

## Tests

```bash
go test ./... -race
cd web && npx tsc --noEmit && npm run build && npm test -- --run
```

Todos los paquetes pasan con race detector. 44 tests de UI cubren OpportunityFeed, SpreadChart, TradeHistory, PriceTable, StatusBar, PnLChart, useMarketSocket.

Coverage en [`docs/arquitectura.md#tests`](docs/arquitectura.md#tests).

## Estructura del proyecto

```
cmd/server/         entry point + processing loop
internal/
  backtest/         runner determinístico + replay clock
  depth/            L2 sintético + walk-the-book + VWAP
  engine/           coordinador + priority queue + latency tracker
  exchange/         10 connectors WS + tests de parseMessage
  executor/         executors per-strategy
  feed/             agregador fan-in
  metrics/          LatencyTracker compartido
  model/            SpreadModel Welford + ScoreSignal helper
  recorder/         WAL SQLite de PriceUpdates
  risk/             circuit breaker + position caps
  server/           hub WS + REST API + handlers
  sizing/           Kelly estimator
  store/            persistencia SQLite
  strategy/
    spatial/        cross-exchange BTC arb (core, on-spec)
    funding/        funding-rate arb BTC perp
    triangular/     ciclo 3-leg (USDT→BTC→ETH→USDT)
  types/            PriceUpdate, Opportunity, Trade, OrderBookLevel
  uptime/           tracker uptime per-exchange
  wallet/           balances simulados
config/             configuración por env
web/                dashboard React + Vite
docs/               documentación detallada
openapi.yaml        contrato REST OpenAPI 3.0
```

## Licencia

MIT.
