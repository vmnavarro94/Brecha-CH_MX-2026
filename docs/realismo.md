# Qué es real, qué es simulado

Documento de honestidad técnica. Para que un evaluador entienda exactamente dónde termina el dato real y dónde empieza el modelado. Sin esta distinción, los números del dashboard (P&L, Sharpe, hit rate) son engañosos.

## TL;DR

| Componente | Origen |
|------------|--------|
| Precios BBO (bid/ask) de los 10 exchanges | **Real**: WebSocket en vivo a cada venue. |
| L2 depth en Binance, Bybit, OKX | **Real**: top 5/20/50 niveles del libro real. |
| L2 depth en los otros 7 exchanges | **Simulado**: generado alrededor del BBO con PRNG seedeado. |
| Spreads cross-exchange | **Real**: derivados de precios reales. Los z-scores que ves son cálculos reales sobre data real. |
| Fees por exchange (taker, withdrawal, network bps) | **Reales por default**: valores retail publicados por cada venue. Editables en vivo. |
| Ejecución de trades | **Simulada**: no se mandan órdenes reales. El executor camina el L2 (real o sintético) y calcula fills determinísticamente. |
| Wallet balances (USDT y BTC por exchange) | **Simulados**: `INITIAL_USDT_PER_EXCHANGE` y `INITIAL_BTC_PER_EXCHANGE` per-exchange. No conectados a cuentas reales. |
| Slippage adicional sobre el buy ask | **Modelado**: fracción configurable por venue. No medido contra trades reales. |
| Withdrawal cost (BTC entre exchanges) | **Modelado**: fee fija conocida por venue, no se mueve BTC real. |
| Network latency cost | **Modelado**: basis points por leg, escalado por edad del counterparty. Aproximación del slippage por drift durante el round-trip WS. |
| Funding rates por exchange | **Sintéticos**: wave generator con noise determinístico. No son funding rates reales. |
| Cycle triangular (USDT→BTC→ETH→USDT) | **Mixto**: BTC/USDT real, ETH/USDT seedeado + drift, BTC/ETH seedeado + drift. Sólo el primer leg es real. |

## En detalle

### Lo que SÍ es real

#### Conexiones a exchanges

Los 10 connectors mantienen WebSocket abiertos a los endpoints **públicos** reales de cada venue:

```
Binance:    wss://stream.binance.com:9443
Kraken:     wss://ws.kraken.com
Bybit:      wss://stream.bybit.com/v5/public/spot
OKX:        wss://ws.okx.com:8443/ws/v5/public
Gate.io:    wss://api.gateio.ws/ws/v4/
MEXC:       wss://wbs.mexc.com/ws
Bitget:     wss://ws.bitget.com/v2/ws/public
HTX:        wss://api.huobi.pro/ws (gzip)
Crypto.com: wss://stream.crypto.com/v2/market
KuCoin:     wss://ws-api-spot.kucoin.com (token-based)
```

Cada tick que recibís en el dashboard pasó por la red real, normalizado a `types.PriceUpdate`. Si Binance se cae, vas a ver "Stale" en el panel PriceTable.

#### Precios BBO

`bid`, `ask`, `bidSize`, `askSize` son los publicados por cada exchange en vivo. La columna "Spread" de PriceTable es real. El "best bid / cheapest ask" highlight es real.

#### L2 depth en Binance / Bybit / OKX

Estos tres venues entregan order book real por canales dedicados:
- Binance: `btcusdt@depth20@100ms` (top 20 niveles cada 100ms)
- Bybit: `orderbook.50.BTCUSDT` (top 50, snapshot + deltas)
- OKX: `books5:BTC-USDT` (top 5, snapshot)

El walk-the-book del executor para opps que involucran estos exchanges camina libros reales.

#### Spreads y SpreadModel

El SpreadModel (Welford μ/σ) se entrena con spreads REALES `(sellBid - buyAsk) / buyAsk`. Los z-scores que ves en el chart son anomalías estadísticas reales sobre data real. El sistema **detecta** oportunidades reales — solo no las **ejecuta** contra el mercado.

#### Fees

`internal/exchange/fees.go` tiene los valores retail reales por venue (taker fee, withdrawal cost en BTC, network latency bps). Sacados de la docs pública de cada exchange. Editables en vivo desde el TweaksPanel pero los defaults son los reales.

---

### Lo que está simulado pero es realista

#### Ejecución de trades

El executor **no manda órdenes** a los exchanges. Cuando una opp gana el priority queue:

1. Toma el L2 book (real o sintético) del buy-side.
2. Camina niveles acumulando VWAP hasta llenar el target volume.
3. Toma el L2 del sell-side.
4. Camina y matchea contra el volumen ya filleado.
5. Si el sell no matchea todo, reversa atómica al costo VWAP exacto.
6. Persiste el `Trade` con `BuyPrice`, `SellPrice`, `Volume`, `NetProfit`, `Fees`, `Slippage`.

**Si lo conectaras a una cuenta real con la misma lógica de execution**, esto sería el flow exacto. La math está bien. Lo único que cambiaría es:
- Las órdenes pueden no fillearse al precio mostrado (slippage real > modelado).
- El bid/ask puede haber cambiado durante el round-trip de la orden.
- Race conditions con otros bots compitiendo por el mismo opp.

El demo NO modela estos efectos en su totalidad — modela algunos via `SlippageFactor` y `NetworkLatencyBps`, pero esos coeficientes son aproximaciones.

#### Wallet balances

`INITIAL_USDT_PER_EXCHANGE = 10000` y `INITIAL_BTC_PER_EXCHANGE = 0.1` son balances simulados por exchange al boot. El `internal/wallet` package los maneja como contadores in-memory. Cada trade simulado:

- Debita USDT del buy exchange.
- Acredita BTC en el buy exchange.
- Debita BTC del sell exchange.
- Acredita USDT (post-fees) en el sell exchange.

**No hay conexión a APIs autenticadas de los exchanges.** El bot nunca se loguea. Solo lee canales públicos.

#### Costos modelados

| Costo | Cómo se modela | Cómo sería en real |
|-------|----------------|--------------------|
| **Taker fee** | Real: el % publicado por el venue. | Mismo número. Match perfecto. |
| **Slippage factor** | Constante por exchange (ej. 0.02%). | Variable según book depth, volatility, order size. El modelo es una aproximación. |
| **Withdrawal cost** | Fee fija en BTC (publicada). | Mismo número, pero hay también tiempo de confirmación (~10 min) que no se modela. |
| **Network latency bps** | Constante por exchange. Escalada por edad del counterparty (`ageFactor = 1 + ms/1000`, cap 3×). | Approximación. En real depende de la ruta de red y de cuánto driftó el precio del sell-side. |

El modelo está construido para que `NetProfit` reportado sea **optimista pero plausible**. Conectado a real, las ganancias serían menores por unfilled orders + slippage extra.

---

### Lo que es sintético / generado

#### L2 depth en los otros 7 exchanges

Kraken, Gate.io, MEXC, Bitget, HTX, Crypto.com, KuCoin no exponen L2 fácilmente o requerirían más trabajo de integración. El package `internal/depth` genera un book **alrededor del BBO real**:

```
ask_levels[i].price = bboAsk × (1 + STEP_PCT × i)
ask_levels[i].qty   = U(MIN_QTY, MAX_QTY)   // PRNG seedeado
```

- El **precio** del nivel está anclado a un BBO real. No es totalmente inventado.
- La **cantidad** por nivel es random uniforme. Esto sí es 100% sintético.

El L2 sintético es determinístico bajo el mismo seed → reproducible en tests y backtests.

#### Funding rates

`internal/strategy/funding/funding.go` genera funding rates **sintéticos** con wave generator:

```
rate[exchange_i] = sin(ticks/3 + i) × BaseDifferential + jitter
```

Funding rates reales tienen mecánicas complejas (cobro cada 8h en Binance, premium index + interest, etc.). El demo NO conecta a las APIs de funding. Genera valores oscilando entre venues para que la strategy tenga material que detectar.

**Implicancia**: las opportunities de funding son verosímiles en estructura (un venue paga, otro cobra) pero los números NO son los del funding real del mercado en este momento.

#### Triangular: ETH/USDT y BTC/ETH

El ciclo USDT → BTC → ETH → USDT usa:
- **BTC/USDT**: real, del WebSocket en vivo.
- **ETH/USDT**: arranca en `TRIANGULAR_SEED_REF_PRICE` (default 2000) y drift random walk cada `RefreshEvery`.
- **BTC/ETH**: arranca en `(ethRef / btcMid) × (1 + noise)` y drift independiente.

Si las divergencias entre las tres son lo suficientemente grandes, el ciclo tiene gain positivo. Esa divergencia es **sintética**. En el mercado real, los arbs triangulares existen pero son más raros y duran milisegundos.

**Implicancia**: triangular muestra que la infra soporta multi-strategy con scoring + ejecución, pero los opps detectados no son market events reales.

---

### Backtest

El recorder graba `PriceUpdate` reales del agregador a la WAL SQLite. El runner los replea contra strategies determinísticamente. **El backtest es real en cuanto a los precios reales que fluyeron** durante el período grabado. Lo simulado del backtest:

- Las strategies re-detectan, así que los opps no son necesariamente los mismos del live (cambios de threshold, de fees, de seed, producen diferencias).
- Funding y triangular usan PRNG con seed, así que reproducen exactamente las mismas señales sintéticas.
- El executor del replay es el mismo simulado del live.

**Sharpe / MaxDD / HitRate son métricas reales** computadas sobre trades simulados que respondieron a precios reales.

---

## Cómo lo presentás a los jueces

Si te preguntan **"¿esto está conectado a un exchange real?"**, la respuesta honesta es:

> **Sí en lectura, no en escritura.** Los 10 connectors leen precios reales de los exchanges en tiempo real, vía sus WebSocket públicos. El motor detecta oportunidades reales sobre data real. Lo que NO hacemos es mandar órdenes — el executor es una simulación determinística que aplica las fees reales documentadas, walks el L2 (real en 3 venues, sintético en 7) y registra el trade como hubiera quedado si se hubiera ejecutado.

Si te preguntan **"¿el P&L que ves es real?"**:

> **Es el P&L que tendrías** si las órdenes se ejecutaran a los precios mostrados, con las fees mostradas, sin slippage adicional, sin race con otros bots y con liquidez suficiente. En la realidad sería menor. Cuánto menor depende del slippage real, que el modelo aproxima con el `SlippageFactor` por venue.

Si te preguntan **"¿por qué la win rate es 100%?"**:

> Porque el executor solo emite `Trade` cuando ya pasó todas las checks: `netProfit > 0`, `netPct >= adaptiveMin`, risk manager aceptó, walk-the-book llenó el target. Es determinístico por construcción. La win rate empezaría a bajar con: slippage agresivo, partial fills más comunes, throttling de fills entre exchanges, race conditions reales.

Si te preguntan **"¿qué pasaría si lo conectara a Binance hoy?"**:

> Toda la lógica de detección + scoring + sizing es portable. Lo que necesitarías agregar: cliente REST autenticado para mandar órdenes, parsing de fills reales (parciales, slippage, rejection), reconciliación de wallet vs lo que diga el exchange, y manejo de errores específicos de venue. Las strategies, el SpreadModel, el Kelly, el correlation penalty y el adaptive threshold quedan igual.
