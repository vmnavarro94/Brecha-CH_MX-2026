# Configuración

Todas las variables son env vars con defaults. Se pueden setear via `.env`, `docker-compose.yml`, o `export VAR=value` antes de `go run`. Algunas son editables en vivo desde el TweaksPanel del dashboard (las marcadas con ✏️).

## Modo y server

| Variable | Default | Propósito |
|----------|---------|-----------|
| `PORT` | `8080` | Puerto del servidor HTTP. |
| `DEMO_MODE` | `false` | Si `true`, habilita el TweaksPanel del dashboard para editar fees en vivo. **No afecta a las fees por sí mismo** — las defaults son las retail reales independientemente del modo. |
| `ALLOWED_ORIGIN` | `*` | Header `Access-Control-Allow-Origin` para CORS. |
| `DATA_DIR` | `./data` | Donde vive el archivo SQLite. En Docker es `/data` montado como volumen. |

## Strategy: Spatial (core)

| Variable | Default | Propósito | Editable en vivo |
|----------|---------|-----------|------------------|
| `MIN_NET_PROFIT_PCT` | `0.0015` | Umbral mínimo de `netPct` para enqueueing (legacy; usá los SPATIAL_* abajo). | ✏️ |
| `SPATIAL_BASE_MIN_NET_PROFIT_PCT` | `0.0015` | Threshold base del adaptive threshold spatial. | — |
| `SPATIAL_ADAPTIVE_COEFF` | `1.0` | Multiplicador de `spreadStd` para el threshold adaptativo. Más alto = más conservador en mercados volátiles. | — |
| `MAX_POSITION_USDT` | `1000` | Cap de posición por trade cuando Kelly no calienta. Una vez Kelly tiene muestras, el cap efectivo es `Kelly.Fraction() × MAX_POSITION_USDT`. | ✏️ |
| `STALENESS_THRESHOLD_MS` | `2000` | Counterparties con `now - ReceivedAt` mayor son descartados del Detect. | ✏️ |
| `EXECUTION_INTERVAL_MS` | `100` | Cada cuánto el executor saca el top del heap. Más bajo = más reactivo, más overhead. | ✏️ |
| `OPPORTUNITY_TTL_MS` | `500` | Si una opp lleva más de esto en el heap sin dequeuearse, se descarta. | — |

### Spatial: Kelly sizing

| Variable | Default | Propósito |
|----------|---------|-----------|
| `KELLY_MIN_SAMPLES` | `10` | Trades realizados antes de que Kelly empiece a sizear. Hasta entonces usa cap plano. |
| `KELLY_MAX_FRACTION` | `0.25` | Cap superior del Kelly fraction. Quarter-Kelly conservador. |

### Spatial: Correlation penalty

| Variable | Default | Propósito |
|----------|---------|-----------|
| `CORR_WINDOW_N` | `50` | Tamaño del ring buffer de últimos trades. |
| `CORR_PENALTY_WEIGHT` | `0.3` | Intensidad del penalty cuando se repite un exchange en el par. |

### Spatial: Imbalance penalty

| Variable | Default | Propósito |
|----------|---------|-----------|
| `IMBALANCE_PENALTY_WEIGHT` | `0.15` | Penalty al score por ask-heavy sell-side book. |

## Strategy: Funding

| Variable | Default | Propósito |
|----------|---------|-----------|
| `FUNDING_ENABLED` | `true` | Habilita la strategy funding. |
| `FUNDING_THRESHOLD` | `0.0008` | Diferencial mínimo de funding rate (entre venues) para emitir opp. |
| `FUNDING_POLL_INTERVAL` | `2s` | Cada cuánto la goroutine refresca los synthetic rates. |
| `FUNDING_EMIT_COOLDOWN` | `5s` | Cooldown por par. |
| `FUNDING_NOTIONAL` | `10000` | Tamaño nominal por trade en USDT. |
| `FUNDING_BASE_DIFFERENTIAL` | `0.001` | Amplitud del wave generator sintético. |
| `FUNDING_SEED` | `42` | PRNG seed. |

## Strategy: Triangular

| Variable | Default | Propósito |
|----------|---------|-----------|
| `TRIANGULAR_ENABLED` | `true` | Habilita la strategy. Set a `false` para corrida estrictamente BTC-only. |
| `TRIANGULAR_NOISE_RANGE` | `0.005` | Range del PRNG noise sobre el ETH ref price (0.5%). |
| `TRIANGULAR_SEED_REF_PRICE` | `2000.0` | Precio de referencia inicial ETH/USDT. |
| `TRIANGULAR_NOTIONAL` | `1000.0` | Tamaño nominal del ciclo en USDT. |
| `TRIANGULAR_TAKER_FEE` | `0.0001` | Fee por leg (3 legs por ciclo). |
| `TRIANGULAR_SEED` | `42` | PRNG seed. |

## Risk: Circuit breaker

| Variable | Default | Propósito | Editable en vivo |
|----------|---------|-----------|------------------|
| `CIRCUIT_BREAKER_N` | `5` | Trades evaluados para racha de pérdidas. | ✏️ |
| `CIRCUIT_BREAKER_LOSS_PCT` | `-0.005` | Si la suma de los últimos N net loss cae por debajo, pausa. | ✏️ |
| `CIRCUIT_BREAKER_PAUSE_MINUTES` | `5` | Cuántos minutos pausa el executor. | — |

## Wallet inicial

| Variable | Default | Propósito |
|----------|---------|-----------|
| `INITIAL_USDT_PER_EXCHANGE` | `10000` | Balance simulado USDT por venue al boot. |
| `INITIAL_BTC_PER_EXCHANGE` | `0.1` | Balance simulado BTC por venue. |

## Profundidad (L2)

| Variable | Default | Propósito |
|----------|---------|-----------|
| `DEPTH_LEVELS` | `5` | Niveles por lado del L2 sintético (cuando no hay L2 real disponible). |
| `DEPTH_STEP_PCT` | `0.0001` | Paso porcentual por nivel. |
| `DEPTH_MIN_QTY_BTC` | `0.005` | Cantidad mínima por nivel (uniforme). |
| `DEPTH_MAX_QTY_BTC` | `0.025` | Cantidad máxima por nivel. |

Binance, Bybit y OKX **siempre usan L2 real** (subscripción directa a depth/orderbook), ignorando estos parámetros.

## Fees retail (per-exchange)

Defaults en `internal/exchange/fees.go`. **Editables en vivo via TweaksPanel** cuando `DEMO_MODE=true`. Ver `docs/exchanges.md` para los valores reales por venue.

| Variable | Editable |
|----------|----------|
| Fee tables per-exchange (taker, slippage, withdrawal, network-latency bps) | ✏️ |

---

## Tuning recipes

### "Quiero más volumen de trades, no me importan opps marginales"

```bash
MIN_NET_PROFIT_PCT=0.0005           # 5 bps en vez de 15
SPATIAL_BASE_MIN_NET_PROFIT_PCT=0.0005
EXECUTION_INTERVAL_MS=50            # ejecutar 2× más rápido
```

### "Quiero conservador, alta tasa de acierto"

```bash
MIN_NET_PROFIT_PCT=0.003            # 30 bps
SPATIAL_ADAPTIVE_COEFF=2.0          # más reactivo a volatilidad
KELLY_MAX_FRACTION=0.10             # eighth-Kelly
CORR_PENALTY_WEIGHT=0.5             # más reducción si se repite par
```

### "Quiero correr solo spatial (modo strict BTC)"

```bash
TRIANGULAR_ENABLED=false
FUNDING_ENABLED=false
```

### "Quiero forzar circuit breaker para demo"

```bash
CIRCUIT_BREAKER_N=3                 # 3 trades en vez de 5
CIRCUIT_BREAKER_LOSS_PCT=-0.001     # threshold 0.1% en vez de 0.5%
CIRCUIT_BREAKER_PAUSE_MINUTES=1     # pausa solo 1 minuto
```

Para activar el CB sin esperar pérdidas reales: editá las fees a algo absurdo (taker 1%) desde el TweaksPanel y forzá pérdidas en cadena.

### "Quiero ver opps de triangular más seguido"

```bash
TRIANGULAR_NOTIONAL=5000            # más capital → opps con netProfit más alto
TRIANGULAR_NOISE_RANGE=0.01         # más noise → más probabilidad de ciclos positivos
```

---

## Cambios en vivo via API

Todo lo marcado con ✏️ se puede modificar via `PATCH /api/config`:

```bash
curl -X PATCH http://localhost:8088/api/config \
  -H 'Content-Type: application/json' \
  -d '{
    "min_net_profit_pct": 0.0010,
    "max_position_usdt": 2000,
    "execution_interval_ms": 80,
    "circuit_breaker_n": 3,
    "circuit_breaker_loss_pct": -0.002
  }'
```

La respuesta es el estado completo de la config aplicada. Los cambios surten efecto al siguiente `ProcessUpdate` (próximo tick).

Las fees también se pueden patchear:

```bash
curl -X PATCH http://localhost:8088/api/config \
  -H 'Content-Type: application/json' \
  -d '{
    "fees": {
      "binance": { "taker_fee": 0.0008, "slippage_factor": 0.0001 },
      "kraken":  { "taker_fee": 0.0020 }
    }
  }'
```

Los campos no enviados quedan como estaban — patch es shallow merge por exchange.
