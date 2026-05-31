# Exchanges y connectors

Brecha conecta a 10 exchanges de forma paralela. Cada uno tiene una goroutine dedicada que mantiene un WebSocket abierto, parsea su formato propio, y normaliza a `types.PriceUpdate`.

## Tabla maestra

| Exchange | Taker fee | Withdrawal | Net-lat (bps) | Canal WS | L2 real |
|----------|----------:|-----------:|--------------:|----------|--------:|
| Binance | 0.10% | 0.00020 BTC | 1 | `btcusdt@bookTicker` + `depth20@100ms` | ✓ |
| Kraken | 0.26% | 0.00005 BTC | 2 | v1 ticker array (XBT/USDT) | — |
| Bybit | 0.10% | 0.00050 BTC | 2 | `orderbook.50.BTCUSDT` | ✓ |
| OKX | 0.10% | 0.00040 BTC | 2 | `tickers` + `books5:BTC-USDT` | ✓ |
| Gate.io | 0.20% | 0.00050 BTC | 3 | `spot.book_ticker BTC_USDT` | — |
| MEXC | 0.20% | 0.00050 BTC | 3 | `spot@public.bookTicker.v3.api@BTCUSDT` | — |
| Bitget | 0.10% | 0.00030 BTC | 2 | `books1 BTCUSDT_SPBL` | — |
| HTX | 0.20% | 0.00010 BTC | 3 | `market.btcusdt.bbo` (gzip) | — |
| Crypto.com | 0.25% | 0.00006 BTC | 2 | `ticker.BTC_USDT` | — |
| KuCoin | 0.10% | 0.00050 BTC | 3 | `/market/ticker:BTC-USDT` | — |

Las fees son las retail reales documentadas en `internal/exchange/fees.go`. Editables en vivo via TweaksPanel (cuando `DEMO_MODE=true`) o via `PATCH /api/config`.

## Categorías de costo

Cada exchange tiene 4 categorías de costo modeladas:

| Campo | Significa | Unidad |
|-------|-----------|--------|
| `TakerFee` | Fee del exchange por orden agresiva. | Fracción del notional (0.001 = 0.1%). |
| `SlippageFactor` | Slippage adicional sobre el buy ask, modelando que el precio publicado no es el ejecutado. | Fracción del buyAsk. |
| `WithdrawalBTC` | Costo de mover BTC del buy exchange al sell exchange. | BTC absoluto. |
| `NetworkLatencyBps` | Slippage implícito del round-trip WS, escalado por edad del counterparty. | Basis points (1 bp = 0.0001). |

Las 4 se aplican en la fórmula de spatial. Ver `docs/estrategias.md#modelo-de-costos`.

## L2 real vs sintético

Tres exchanges entregan order book L2 real por WebSocket:

| Exchange | Canal L2 |
|----------|----------|
| Binance | `btcusdt@depth20@100ms` (top 20 niveles, refresco 100ms) |
| Bybit | `orderbook.50.BTCUSDT` (top 50 niveles, snapshot + delta) |
| OKX | `books5:BTC-USDT` (top 5 niveles, snapshot completo cada update) |

El resto usan un L2 **sintético** generado determinísticamente alrededor del BBO:

```
ask_levels[i] = bboAsk × (1 + STEP_PCT × i)   con qty ~ U(MIN_QTY, MAX_QTY)
bid_levels[i] = bboBid × (1 − STEP_PCT × i)   con qty ~ U(MIN_QTY, MAX_QTY)
```

`rand.Source` seedeado para que sea reproducible en tests.

Por qué no L2 real en todos: rewriting los 10 connectors a L2 hubiera tomado semanas para marginal accuracy gain en simulación. La fidelidad importante (top-3 venues por volumen) está cubierta.

## Mensajes por connector

Cada connector implementa `parseMessage(msg []byte) (types.PriceUpdate, bool)` aislado, testeable contra frames de ejemplo capturados de cada exchange.

### Binance — formato

```json
{
  "u": 12345,
  "s": "BTCUSDT",
  "b": "74130.16",
  "B": "0.5",
  "a": "74130.17",
  "A": "0.3"
}
```

Campos: `b` bid, `B` bid size, `a` ask, `A` ask size.

### Kraken — formato

Array indexed (no JSON object):

```json
[
  340,
  { "a": ["74150.0", 1, "0.5"], "b": ["74148.0", 2, "0.8"] },
  "ticker",
  "XBT/USDT"
]
```

Note `XBT` no `BTC`. El connector lo mapea.

### Bybit — formato

```json
{
  "topic": "orderbook.50.BTCUSDT",
  "type": "snapshot",
  "data": {
    "s": "BTCUSDT",
    "b": [["74121.10", "0.5"], ...],
    "a": [["74121.20", "0.3"], ...]
  }
}
```

### OKX — formato

```json
{
  "arg": { "channel": "tickers", "instId": "BTC-USDT" },
  "data": [{
    "bidPx": "74127.40",
    "askPx": "74127.50",
    "bidSz": "0.5",
    "askSz": "0.3"
  }]
}
```

### Otros

Cada uno tiene su quirk. Ver `internal/exchange/{exchange}.go` y `internal/exchange/{exchange}_test.go` para detalles + frames de ejemplo.

## Reconexión

Todos los connectors implementan reconnect con backoff exponencial:

```
delay = min(1s × 2^attempt, 60s)
```

Cuando reconecta, re-suscribe a los canales originales. Si el exchange rate-limita la reconexión (más común en HTX y Kraken), el backoff la maneja sin perder otros connectors.

El ReconnectBanner del dashboard muestra el estado:
- Verde "Connected" cuando WS abierto.
- Naranja "Reconnecting in Xs…" durante backoff.
- Rojo "Disconnected" si el server cierra el WS.

## Uptime tracking

`internal/uptime/tracker.go` muestrea cada exchange a 1 Hz. Cada exchange acumula `(fresh_seconds, total_seconds)` durante la sesión.

`fresh` se define como `now - ReceivedAt < 10s`. Si pasa de eso, el sample cuenta como `stale`.

El % de uptime se publica como evento `uptime_stats` cada 250ms (throttled) y se muestra en la PriceTable.

## Parse latency

Cada connector tiene su `LatencyTracker` propio que mide los nanosegundos entre `WebSocket message received` y `PriceUpdate emitted`. Se expone via `/api/health`:

```json
{
  "binance": {
    "parse_p50_us": 12.5,
    "parse_p99_us": 45.0
  }
}
```

Diagnóstico:
- p50 alto (>100µs) en un connector específico: probable bug de parsing.
- p99 muy alto pero p50 OK: el GC del runtime de Go genera spikes ocasionales.

## MEXC: bloqueo regional

MEXC bloquea sub no autenticada al canal de BBO desde varias regiones (incluyendo MX). El connector loggea:

```
Not Subscribed successfully! [spot@public.bookTicker.v3.api@BTCUSDT]. Reason: Blocked!
```

El panel PriceTable muestra "Esperando…" para MEXC en estos casos. No es un bug del bot — es restricción del exchange. Solución: usar VPN al país de la cuenta o autenticar el WS (no implementado en el demo).

## Agregar un nuevo exchange

Para sumar un connector:

1. Crear `internal/exchange/<nombre>.go` que implemente:
   ```go
   func (c *Connector) Name() string
   func (c *Connector) Start(ctx context.Context, ch chan<- types.PriceUpdate) error
   func (c *Connector) ParseMessage(msg []byte) (types.PriceUpdate, bool)
   ```
2. Test con frames de ejemplo en `internal/exchange/<nombre>_test.go`.
3. Sumar la fee al map en `internal/exchange/fees.go`.
4. Wire en `cmd/server/main.go` agregando a `connectors`.
5. Sumar el exchange a la enum del frontend en `web/src/types/api.ts`.

Si el exchange entrega L2 real, además implementar:

```go
func (c *Connector) BookUpdates() <-chan types.BookUpdate
```

Y wire al `BookConnector` interface.
