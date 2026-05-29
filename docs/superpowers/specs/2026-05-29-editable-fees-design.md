# Editable Fees in Demo Mode

**Date:** 2026-05-29
**Status:** Approved

## Goal

Allow judges to edit per-exchange taker fees and slippage directly from the TweaksPanel while in demo mode, so they can demonstrate different profit scenarios without mocking exchanges.

## Scope

Demo-mode only. Fees become read-only again when demo_mode is false. No new UI components — extend existing fee table.

## Backend

### `internal/server/api.go`

Add `Fees` to `ConfigPatch`:

```go
type FeeInfoPatch struct {
    TakerFee *float64 `json:"taker_fee"`
    Slippage  *float64 `json:"slippage"`
}

type ConfigPatch struct {
    // ... existing fields ...
    Fees map[string]*FeeInfoPatch `json:"fees,omitempty"`
}
```

Pointer fields allow updating only one of the two values per exchange.

### `cmd/server/main.go` — `patchConfigFn`

When `patch.Fees != nil`, iterate over the provided entries and merge into `liveCfg.Fees`, then rebuild the engine fee map and call `eng.SetFees()`:

```go
if patch.Fees != nil {
    for name, fp := range patch.Fees {
        if fp == nil {
            continue
        }
        cur := liveCfg.Fees[name] // FeeInfo{TakerFee, Slippage}
        if fp.TakerFee != nil {
            cur.TakerFee = *fp.TakerFee
        }
        if fp.Slippage != nil {
            cur.Slippage = *fp.Slippage
        }
        liveCfg.Fees[name] = cur
    }
    newFees := make(map[string]engine.FeeConfig, len(liveCfg.Fees))
    for name, f := range liveCfg.Fees {
        newFees[name] = engine.FeeConfig{TakerFee: f.TakerFee, SlippageFactor: f.Slippage}
    }
    eng.SetFees(newFees)
}
```

## Frontend — `TweaksPanel.tsx`

### Fee cell behavior

- `demo_mode == true`: taker_fee and slippage cells render as `<input type="number">` with `step="0.0001"` and `min="0"` and `max="0.05"`
- `demo_mode == false`: read-only text (same as today)

### Input values

Store and send values as raw decimals (e.g., `0.001` for 0.1%). Display with `toFixed(4)` in read-only mode, raw value in inputs. No double-conversion between % and decimal.

### Patch on commit

- `onChange` → update local `cfg.fees[name]` in component state (optimistic)
- `onBlur` + `onKeyDown Enter` → send PATCH `{fees: {[name]: {taker_fee: v, slippage: v}}}`
- Debounce not needed here (commit-on-blur is already lazy enough)

### Reset behavior

The existing Reset button sends `patch(DEFAULTS)` which includes `demo_mode: true`. The backend's `demo_mode` branch already resets fees to `exchange.DemoFees` and updates `liveCfg.Fees`. No change needed to the Reset button, but the component must re-fetch config after reset (already done via `setCfg(DEFAULTS)` + `patch()` — this is a bug: local state goes to DEFAULTS but fees don't update locally). Fix: after reset patch resolves, re-fetch `/api/config` and set state.

## Error handling

No special error handling. If PATCH fails, the local state is already updated (optimistic). On next `/api/config` fetch (page load) the server state is authoritative.

## Testing

- Unit: `patchConfigFn` with fee patch → engine fees updated
- Integration (existing api_test.go pattern): PATCH `/api/config` with fees → GET returns updated fees
- Frontend: no new tests required (TweaksPanel is dev-only, not in production build)
