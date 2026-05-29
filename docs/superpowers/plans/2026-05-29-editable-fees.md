# Editable Fees in Demo Mode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow judges to edit per-exchange taker fees and slippage from TweaksPanel while in demo mode, without restarting the server or mocking exchanges.

**Architecture:** Add `FeeInfoPatch` struct and `Fees` field to `ConfigPatch` in `api.go` so the PATCH endpoint can receive per-exchange fee overrides. Wire the merge logic into `patchConfigFn` in `main.go` which calls `eng.SetFees()`. In the frontend, make the fee table cells editable `<input>` elements when `demo_mode` is true.

**Tech Stack:** Go 1.22, net/http, React 18, TypeScript, Zustand

---

## Files

- Modify: `internal/server/api.go` — add `FeeInfoPatch` struct and `Fees` field to `ConfigPatch`
- Modify: `internal/server/api_test.go` — add test helpers and fee patch test
- Modify: `cmd/server/main.go` — handle `patch.Fees` in `patchConfigFn`
- Modify: `web/src/components/TweaksPanel.tsx` — editable fee cells + reset fix
- Modify: `web/src/App.css` — add `.tw-fee-input` styles

---

### Task 1: Write failing test for fee PATCH

**Files:**
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Add helper `newAPIWithConfigFn` and `patchJSON`**

In `internal/server/api_test.go`, after the existing `newAPI` helper, add:

```go
func newAPIWithConfigFn(
	t *testing.T,
	st *store.Store,
	rm *risk.RiskManager,
	getCfg func() server.ConfigSnapshot,
	patchCfg func(server.ConfigPatch) server.ConfigSnapshot,
) http.Handler {
	t.Helper()
	return server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		getCfg,
		patchCfg,
		"http://localhost:3000", 3,
	)
}

func patchJSON(t *testing.T, handler http.Handler, path string, body string) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp, v
}
```

Add `"strings"` to the import block.

- [ ] **Step 2: Write the failing test**

In `internal/server/api_test.go`, add after the CORS test:

```go
// --- PATCH /api/config (fees) ---

func TestAPIConfig_PatchFees(t *testing.T) {
	st, rm := newTestComponents(t)

	snap := server.ConfigSnapshot{
		Fees: map[string]server.FeeInfo{
			"binance": {TakerFee: 0.001, Slippage: 0.0002},
		},
	}

	handler := newAPIWithConfigFn(t, st, rm,
		func() server.ConfigSnapshot { return snap },
		func(p server.ConfigPatch) server.ConfigSnapshot {
			if p.Fees != nil {
				for name, fp := range p.Fees {
					if fp == nil {
						continue
					}
					cur := snap.Fees[name]
					if fp.TakerFee != nil {
						cur.TakerFee = *fp.TakerFee
					}
					if fp.Slippage != nil {
						cur.Slippage = *fp.Slippage
					}
					snap.Fees[name] = cur
				}
			}
			return snap
		},
	)

	_, body := patchJSON(t, handler, "/api/config",
		`{"fees":{"binance":{"taker_fee":0.0005}}}`,
	)

	fees, ok := body["fees"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees key in response")
	}
	binance, ok := fees["binance"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees.binance in response")
	}
	if binance["taker_fee"].(float64) != 0.0005 {
		t.Errorf("expected taker_fee=0.0005, got %v", binance["taker_fee"])
	}
	if binance["slippage"].(float64) != 0.0002 {
		t.Errorf("expected slippage=0.0002 (unchanged), got %v", binance["slippage"])
	}
}

func TestAPIConfig_PatchFees_SlippageOnly(t *testing.T) {
	st, rm := newTestComponents(t)

	snap := server.ConfigSnapshot{
		Fees: map[string]server.FeeInfo{
			"okx": {TakerFee: 0.001, Slippage: 0.0002},
		},
	}

	handler := newAPIWithConfigFn(t, st, rm,
		func() server.ConfigSnapshot { return snap },
		func(p server.ConfigPatch) server.ConfigSnapshot {
			if p.Fees != nil {
				for name, fp := range p.Fees {
					if fp == nil {
						continue
					}
					cur := snap.Fees[name]
					if fp.TakerFee != nil {
						cur.TakerFee = *fp.TakerFee
					}
					if fp.Slippage != nil {
						cur.Slippage = *fp.Slippage
					}
					snap.Fees[name] = cur
				}
			}
			return snap
		},
	)

	_, body := patchJSON(t, handler, "/api/config",
		`{"fees":{"okx":{"slippage":0.0005}}}`,
	)

	fees := body["fees"].(map[string]interface{})
	okx := fees["okx"].(map[string]interface{})

	if okx["taker_fee"].(float64) != 0.001 {
		t.Errorf("expected taker_fee=0.001 (unchanged), got %v", okx["taker_fee"])
	}
	if okx["slippage"].(float64) != 0.0005 {
		t.Errorf("expected slippage=0.0005, got %v", okx["slippage"])
	}
}
```

- [ ] **Step 3: Run test — expect compile error**

```bash
cd /home/vnav/coding-challenge-mexico && go test ./internal/server/... 2>&1
```

Expected: compile error: `p.Fees undefined (type ConfigPatch has no field or method Fees)`

---

### Task 2: Add FeeInfoPatch to ConfigPatch (make test GREEN)

**Files:**
- Modify: `internal/server/api.go`

- [ ] **Step 1: Add `FeeInfoPatch` struct and `Fees` field**

In `internal/server/api.go`, after the `ConfigPatch` struct, add `FeeInfoPatch` and add `Fees` to `ConfigPatch`:

Replace the existing `ConfigPatch` struct:

```go
// ConfigPatch carries a partial update for mutable demo parameters.
// Only non-nil fields are applied.
type ConfigPatch struct {
	DemoMode              *bool                       `json:"demo_mode"`
	MinNetProfitPct       *float64                    `json:"min_net_profit_pct"`
	MaxPositionUSDT       *float64                    `json:"max_position_usdt"`
	StalenessThresholdMs  *int                        `json:"staleness_threshold_ms"`
	ExecutionIntervalMs   *int                        `json:"execution_interval_ms"`
	CircuitBreakerN       *int                        `json:"circuit_breaker_n"`
	CircuitBreakerLossPct *float64                    `json:"circuit_breaker_loss_pct"`
	Fees                  map[string]*FeeInfoPatch    `json:"fees,omitempty"`
}

// FeeInfoPatch carries a partial update for a single exchange's fee config.
// Only non-nil fields are applied; the other field keeps its current value.
type FeeInfoPatch struct {
	TakerFee *float64 `json:"taker_fee,omitempty"`
	Slippage  *float64 `json:"slippage,omitempty"`
}
```

- [ ] **Step 2: Run test — expect PASS**

```bash
cd /home/vnav/coding-challenge-mexico && go test ./internal/server/... -run TestAPIConfig_Patch -v 2>&1
```

Expected:
```
--- PASS: TestAPIConfig_PatchFees (0.00s)
--- PASS: TestAPIConfig_PatchFees_SlippageOnly (0.00s)
PASS
```

- [ ] **Step 3: Run all tests to check no regressions**

```bash
cd /home/vnav/coding-challenge-mexico && go test ./... 2>&1
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/api.go internal/server/api_test.go
git commit -m "feat(api): add FeeInfoPatch to ConfigPatch for per-exchange fee editing"
```

---

### Task 3: Wire fee patch in patchConfigFn

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add fee merge logic in patchConfigFn**

In `cmd/server/main.go`, in the `patchConfigFn` closure (around line 200, after the `DemoMode` block), add before the `return liveCfg` statement:

```go
		if patch.Fees != nil {
			for name, fp := range patch.Fees {
				if fp == nil {
					continue
				}
				cur := liveCfg.Fees[name]
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

Place this block AFTER the `if patch.DemoMode != nil { ... }` block and BEFORE `return liveCfg`.

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /home/vnav/coding-challenge-mexico && go build ./cmd/server/... 2>&1
```

Expected: no output (clean build)

- [ ] **Step 3: Run all tests**

```bash
cd /home/vnav/coding-challenge-mexico && go test ./... 2>&1
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(engine): apply per-exchange fee patches from PATCH /api/config"
```

---

### Task 4: Frontend — editable fee inputs

**Files:**
- Modify: `web/src/components/TweaksPanel.tsx`
- Modify: `web/src/App.css`

- [ ] **Step 1: Add `.tw-fee-input` CSS**

In `web/src/App.css`, after `.tw-fee-slip { ... }` (around line 388), add:

```css
.tw-fee-input {
  width: 60px;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--line);
  color: var(--up);
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 0 2px;
  text-align: right;
  outline: none;
  -moz-appearance: textfield;
}

.tw-fee-input::-webkit-inner-spin-button,
.tw-fee-input::-webkit-outer-spin-button {
  -webkit-appearance: none;
}

.tw-fee-input:focus {
  border-bottom-color: var(--orange);
  color: var(--orange);
}
```

- [ ] **Step 2: Replace fee table cells with editable inputs**

In `web/src/components/TweaksPanel.tsx`, replace the entire `{cfg.fees && ...}` block (lines 175–201) with this version:

```tsx
{cfg.fees && Object.keys(cfg.fees).length > 0 && (
  <div className="tw-fee-block">
    <div className="tw-section-label" style={{ marginTop: 2 }}>Comisiones activas</div>
    <table className="tw-fee-table">
      <thead>
        <tr>
          <th>Exchange</th>
          <th>Taker</th>
          <th>Slip</th>
        </tr>
      </thead>
      <tbody>
        {Object.entries(cfg.fees)
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([name, f]) => (
            <tr key={name}>
              <td className="tw-fee-name">{name}</td>
              <td className={cfg.demo_mode ? 'tw-fee-demo' : 'tw-fee-real'}>
                {cfg.demo_mode ? (
                  <input
                    type="number"
                    className="tw-fee-input"
                    value={f.taker_fee}
                    min={0}
                    max={0.05}
                    step={0.0001}
                    onChange={(e) => {
                      const v = parseFloat(e.target.value)
                      if (isNaN(v)) return
                      setCfg((prev) => ({
                        ...prev,
                        fees: { ...prev.fees, [name]: { ...prev.fees![name], taker_fee: v } },
                      }))
                    }}
                    onBlur={(e) => {
                      const v = parseFloat(e.target.value)
                      if (!isNaN(v)) patch({ fees: { [name]: { taker_fee: v, slippage: f.slippage } } })
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        const v = parseFloat((e.target as HTMLInputElement).value)
                        if (!isNaN(v)) patch({ fees: { [name]: { taker_fee: v, slippage: f.slippage } } })
                      }
                    }}
                  />
                ) : (
                  (f.taker_fee * 100).toFixed(4) + '%'
                )}
              </td>
              <td className="tw-fee-slip">
                {cfg.demo_mode ? (
                  <input
                    type="number"
                    className="tw-fee-input"
                    value={f.slippage}
                    min={0}
                    max={0.05}
                    step={0.0001}
                    onChange={(e) => {
                      const v = parseFloat(e.target.value)
                      if (isNaN(v)) return
                      setCfg((prev) => ({
                        ...prev,
                        fees: { ...prev.fees, [name]: { ...prev.fees![name], slippage: v } },
                      }))
                    }}
                    onBlur={(e) => {
                      const v = parseFloat(e.target.value)
                      if (!isNaN(v)) patch({ fees: { [name]: { taker_fee: f.taker_fee, slippage: v } } })
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        const v = parseFloat((e.target as HTMLInputElement).value)
                        if (!isNaN(v)) patch({ fees: { [name]: { taker_fee: f.taker_fee, slippage: v } } })
                      }
                    }}
                  />
                ) : (
                  (f.slippage * 100).toFixed(4) + '%'
                )}
              </td>
            </tr>
          ))}
      </tbody>
    </table>
  </div>
)}
```

Note: `patch()` always sends both fields (`taker_fee` and `slippage`) to satisfy TypeScript's `FeeInfo` type (both required). The unchanged field carries its current value from `f`, so the server sees no diff for it.

- [ ] **Step 3: Fix Reset button to re-fetch config after patch**

In `TweaksPanel.tsx`, replace the Reset button `onClick`:

```tsx
onClick={() => {
  patch(DEFAULTS)
    .then((r) => r.json())
    .then((data: Config) => setCfg(data))
}}
```

The PATCH response IS the server's ConfigSnapshot after applying DEFAULTS (which re-triggers the DemoMode branch in patchConfigFn, resetting fees to DemoFees). So decoding the response gives us the authoritative state including reset fees.

- [ ] **Step 4: TypeScript check**

```bash
cd /home/vnav/coding-challenge-mexico/web && npx tsc --noEmit 2>&1
```

Expected: `TypeScript: No errors found`

- [ ] **Step 5: Build frontend**

```bash
cd /home/vnav/coding-challenge-mexico/web && npm run build 2>&1 | tail -10
```

Expected: clean build, no errors

- [ ] **Step 6: Commit**

```bash
git add web/src/components/TweaksPanel.tsx web/src/App.css
git commit -m "feat(ui): editable fee inputs in TweaksPanel demo mode"
```
