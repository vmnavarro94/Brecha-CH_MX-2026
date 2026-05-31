# Capturas del dashboard

Las imágenes de esta carpeta se usan en la documentación. Se regeneran corriendo el script de captura:

```bash
# Asegurate de que el dashboard esté arriba en localhost:8088
docker compose up -d

# Capturá (puppeteer + chrome headless ya instalados en scripts/node_modules)
node scripts/capture-screenshots.mjs http://localhost:8088 45
```

El segundo argumento es cuántos segundos esperar después de abrir el dashboard antes de capturar. Default 45s — alcanza para que los SpreadModels de spatial y funding lleguen a `MinSamples=30` y empiecen a producir z-scores reales.

## Inventario

| Archivo | Contenido |
|---------|-----------|
| `00-dashboard-full.png` | Dashboard completo en 1600×1100 @ 2x. |
| `01-dashboard-with-picker.png` | Dashboard con el popover de selector de pares abierto. |
| `statusbar.png` | Barra superior: circuit breaker, P&L, win rate, exchanges, trades, latency, throughput, conexión. |
| `spread-chart.png` | Panel Z-score del spread. |
| `price-table.png` | Panel Precios en vivo (BBO + uptime). |
| `pnl-chart.png` | Panel P&L acumulado con stats. |
| `spread-heatmap.png` | Matriz 10×10 de spreads. |
| `opportunity-feed.png` | Feed de oportunidades en vivo. |
| `tweaks-panel.png` | Panel de parámetros editables. |
| `trade-history.png` | Historial paginado de trades. |
| `backtest-panel.png` | Panel Backtest: controles + run history. |
| `pair-picker-open.png` | Popover de selector de pares (zoom). |
| `brecha-logo.svg` | Logo principal usado en el README. |
| `manifest.json` | Metadata de la última corrida del script (url, viewport, timestamp). |

## Convenciones

- Resolución base: 1600×1100 con `deviceScaleFactor: 2` (output 3200×2200 efectivo).
- Formato: PNG con transparencia donde aplica.
- Naming: `kebab-case.png`. Prefijo numérico (`00-`, `01-`) solo para las dos vistas del dashboard completo.

## Si la captura falla

- "selector not found" en un panel: el `id` cambió en el componente — actualizá `PANELS` en `scripts/capture-screenshots.mjs`.
- Captura muestra modelos no listos (warming): subí el segundo argumento a 90 o 120 segundos.
- Captura sin opps en el feed: el threshold `MIN_NET_PROFIT_PCT` puede estar muy alto para el momento; usá fees retail bajas o demo mode antes de capturar.
