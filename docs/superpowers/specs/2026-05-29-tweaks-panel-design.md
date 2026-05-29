# TweaksPanel — Design Spec

**Date:** 2026-05-29
**Status:** Approved

## Goal

Add a third column to the dashboard where judges can modify engine and risk parameters in real time. Clearly labeled as demo-only. Lets evaluators explore how the system responds to different configurations without restarting.

## Layout Change

Current layout: 2 columns (charts left, prices+opportunities right) + full-width trades table.

New layout: 3 columns.

```
[ Z-Score chart      ] [ Prices BBO          ] [ TweaksPanel    ]
[ P&L chart          ] [ Opportunities feed   ] [                ]
[                    ] [                      ] [                ]
[ Trades table (full width)                                      ]
```

The third column is fixed-width (~260px), always visible, does not collapse.

## Parameters Exposed

| Parameter | Control | Range | Default |
|-----------|---------|-------|---------|
| Demo Mode | Toggle | on/off | on |
| Min Net Profit % | Slider | 0% – 0.30% | 0.00% |
| Max Position USDT | Slider | $100 – $5,000 | $1,000 |
| Staleness Threshold | Slider | 500ms – 10,000ms | 2,000ms |
| Execution Interval | Slider | 100ms – 5,000ms | 100ms |
| Circuit Breaker N | Stepper (+/−) | 2 – 10 | 5 |
| Circuit Breaker Loss % | Slider | −0.1% – −5.0% | −0.50% |

## Visual Design

Matches existing Brecha dark theme (IBM Plex Mono, dark backgrounds, green accents).

- Header: `⚙ PARÁMETROS` label + amber `SOLO DEMO` badge
- Warning bar: amber "Cambios se aplican en tiempo real"
- Each param: uppercase label, current value (green), slider or stepper
- Loss threshold slider: red accent (negative value)
- Circuit Breaker section: secondary label separator
- Footer: `↺ Reset a defaults` button — restores all values to their defaults

## Backend Architecture

### Mutable Config

Engine and RiskManager currently hold immutable config structs. Both need setter methods protected by `sync.RWMutex` so parameters can change while the system is running.

**Engine additions** (`internal/engine/engine.go`):
```go
func (e *Engine) SetMinNetProfitPct(v float64)
func (e *Engine) SetMaxPositionUSDT(v float64)
func (e *Engine) SetStalenessThreshold(v time.Duration)
func (e *Engine) SetFees(fees map[string]FeeConfig)
```

`ProcessUpdate` and `DequeueTop` acquire `e.cfgMu.RLock()` when reading config fields.

**RiskManager additions** (`internal/risk/risk.go`):
```go
func (r *RiskManager) SetMinNetProfitPct(v float64)
func (r *RiskManager) SetConsecutiveLossN(n int)
func (r *RiskManager) SetLossThreshold(v float64)
```

These acquire the existing internal mutex.

### New API Endpoint

```
GET  /api/config        → returns current values for all 7 params
PATCH /api/config       → updates one or more params, returns updated state
```

Request body (PATCH):
```json
{
  "demo_mode": true,
  "min_net_profit_pct": 0.0,
  "max_position_usdt": 1000,
  "staleness_threshold_ms": 2000,
  "execution_interval_ms": 100,
  "circuit_breaker_n": 5,
  "circuit_breaker_loss_pct": -0.005
}
```

All fields are optional. Only provided fields are updated.

The `demo_mode` toggle swaps `exchange.DemoFees` ↔ `exchange.Fees` and calls `engine.SetFees(...)`.

`execution_interval_ms` requires resetting the ticker in `runProcessingLoop`. The loop reads from a channel (`configUpdate chan time.Duration`) and resets the ticker when a new interval arrives.

### APIHandler changes

`NewAPIHandler` receives two new dependencies: `*engine.Engine` and `*risk.RiskManager`, plus a `configUpdate chan time.Duration` for the execution interval.

A `RuntimeConfig` struct (in `server` package) holds the current values and is updated on each PATCH. It's used by `GET /api/config` to return current state.

## Frontend Architecture

### New component: `TweaksPanel`

`web/src/components/TweaksPanel.tsx`

- Fetches initial config from `GET /api/config` on mount
- Each control calls `PATCH /api/config` with the changed field on change
- Sliders debounce 150ms before firing the PATCH (avoids flooding on drag)
- Toggle and stepper fire immediately
- No local state for values — reflects server response after each PATCH

### Layout change

`web/src/App.tsx` (or `Shell.tsx` — wherever the main grid is defined):

Add third column. The existing two-column grid becomes three-column. TweaksPanel gets the rightmost column spanning the full height (above trades table).

## Error Handling

- PATCH failures: show a brief amber flash on the affected control, revert its value
- GET failure on mount: panel shows "config unavailable" state, sliders disabled
- No retry logic — next user interaction retries naturally

## Testing

Unit tests not required for this panel (it's a thin UI over config setters). Integration: manually verify each slider updates behavior observable in the dashboard (e.g., raising min profit stops trades, toggling demo mode changes opportunity rate).

The Engine and RiskManager setter methods get unit tests verifying the new value is read correctly by ProcessUpdate/Evaluate after a concurrent Set call.
