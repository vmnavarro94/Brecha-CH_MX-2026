# Documentación de Brecha

Esta carpeta contiene la documentación detallada del motor de arbitraje. El README raíz es el índice y la guía para arrancar; estos archivos profundizan en cada subsistema.

## Índice

| Documento | Para entender… |
|-----------|----------------|
| [`arquitectura.md`](arquitectura.md) | Cómo encajan las capas, dónde corre cada cosa, el processing loop, recorder, modelo de concurrencia. |
| [`estrategias.md`](estrategias.md) | Las tres strategies (spatial, funding, triangular) con diagramas de flujo y modelo de costos por leg. |
| [`scoring.md`](scoring.md) | SpreadModel Welford, fórmula de score, Kelly sizing, correlation penalty y adaptive threshold. |
| [`dashboard.md`](dashboard.md) | Tour panel por panel del dashboard. Cada gráfico, cada color, cada estado, con capturas y ejemplos. |
| [`configuracion.md`](configuracion.md) | Todas las env vars con default, propósito y notas de tuning. |
| [`api.md`](api.md) | Contrato REST + eventos WebSocket. |
| [`backtest.md`](backtest.md) | Recorder WAL + replay determinístico. Métricas computadas. |
| [`exchanges.md`](exchanges.md) | Los 10 connectors, fees retail reales, disponibilidad de L2 real vs sintético. |
| [`realismo.md`](realismo.md) | **Qué es real, qué es simulado, qué es modelado.** Para que los evaluadores entiendan exactamente dónde termina la data real y empieza el modelo. |

## Cómo leer estos docs

Si sos juez evaluando el proyecto en 10 minutos:

1. Mirá [`dashboard.md`](dashboard.md) primero — entendés qué está pasando visualmente.
2. Después [`estrategias.md`](estrategias.md) — entendés cómo se detectan las oportunidades.
3. Si querés profundizar en la matemática, [`scoring.md`](scoring.md).

Si sos developer queriendo modificar el sistema:

1. [`arquitectura.md`](arquitectura.md) para el mapa general.
2. [`api.md`](api.md) para los contratos.
3. [`configuracion.md`](configuracion.md) para tuning.

## Imágenes y diagramas

Las capturas de pantalla del dashboard viven en [`img/`](img/) y se regeneran con:

```bash
cd scripts && node capture-screenshots.mjs
```

El script abre el dashboard con Puppeteer/Chrome headless, espera 45 segundos a que los modelos calienten y los trades fluyan, y captura cada panel + el dashboard completo. Ver [`img/README.md`](img/README.md) para detalles.

Los diagramas Mermaid están embebidos en cada doc y GitHub los renderiza nativamente.
