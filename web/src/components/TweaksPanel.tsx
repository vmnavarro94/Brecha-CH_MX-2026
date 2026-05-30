import { useEffect, useRef, useState } from 'react'

interface FeeInfo {
  taker_fee: number
  slippage: number
  withdrawal_btc: number
  network_latency_bps: number
}

interface Config {
  demo_mode: boolean
  min_net_profit_pct: number
  max_position_usdt: number
  staleness_threshold_ms: number
  execution_interval_ms: number
  circuit_breaker_n: number
  circuit_breaker_loss_pct: number
  fees?: Record<string, FeeInfo>
}

const DEFAULTS: Config = {
  demo_mode: true,
  min_net_profit_pct: 0.0,
  max_position_usdt: 1000,
  staleness_threshold_ms: 2000,
  execution_interval_ms: 100,
  circuit_breaker_n: 5,
  circuit_breaker_loss_pct: -0.005,
}

function patch(partial: Partial<Config>) {
  return fetch('/api/config', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(partial),
  })
}

function fmtPct(v: number) {
  return (v * 100).toFixed(2) + '%'
}

function fmtMs(v: number) {
  return v >= 1000 ? (v / 1000).toFixed(1) + 's' : v + 'ms'
}

interface SliderRowProps {
  label: string
  value: number
  min: number
  max: number
  step: number
  display: string
  accent?: 'down' | 'orange'
  onChange: (v: number) => void
  onCommit: (v: number) => void
}

function SliderRow({ label, value, min, max, step, display, accent, onChange, onCommit }: SliderRowProps) {
  const color = accent === 'down' ? 'var(--down)' : accent === 'orange' ? 'var(--orange)' : 'var(--up)'
  const pct = ((value - min) / (max - min)) * 100

  return (
    <div className="tw-row">
      <div className="tw-row-head">
        <span className="bx-eyebrow">{label}</span>
        <span className="tw-val" style={{ color }}>{display}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        style={{ '--tw-pct': `${pct}%`, '--tw-color': color } as React.CSSProperties}
        onChange={(e) => onChange(parseFloat(e.target.value))}
        onMouseUp={(e) => onCommit(parseFloat((e.target as HTMLInputElement).value))}
        onTouchEnd={(e) => onCommit(parseFloat((e.target as HTMLInputElement).value))}
      />
    </div>
  )
}

interface StepperRowProps {
  label: string
  value: number
  min: number
  max: number
  onChange: (v: number) => void
}

function StepperRow({ label, value, min, max, onChange }: StepperRowProps) {
  return (
    <div className="tw-row">
      <div className="tw-row-head">
        <span className="bx-eyebrow">{label}</span>
        <div className="tw-stepper">
          <button
            className="tw-step-btn"
            disabled={value <= min}
            onClick={() => onChange(Math.max(min, value - 1))}
          >−</button>
          <span className="tw-val" style={{ color: 'var(--orange)', minWidth: 24, textAlign: 'center' }}>{value}</span>
          <button
            className="tw-step-btn"
            disabled={value >= max}
            onClick={() => onChange(Math.min(max, value + 1))}
          >+</button>
        </div>
      </div>
    </div>
  )
}

export default function TweaksPanel() {
  const [cfg, setCfg] = useState<Config>(DEFAULTS)
  const [loading, setLoading] = useState(true)
  const patchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    fetch('/api/config')
      .then((r) => r.json())
      .then((data: Config) => {
        setCfg(data)
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [])

  function applyPatch(partial: Partial<Config>) {
    setCfg((prev) => ({ ...prev, ...partial }))
    patch(partial)
  }

  function debouncedPatch(partial: Partial<Config>, delay = 120) {
    setCfg((prev) => ({ ...prev, ...partial }))
    if (patchTimer.current) clearTimeout(patchTimer.current)
    patchTimer.current = setTimeout(() => patch(partial), delay)
  }

  if (loading) {
    return (
      <div className="bx-panel tw-panel">
        <div className="bx-panel-head">
          <span className="bx-eyebrow">⚙ PARÁMETROS</span>
          <span className="tw-demo-badge">SOLO DEMO</span>
        </div>
        <div className="tw-loading">Cargando…</div>
      </div>
    )
  }

  return (
    <div className="bx-panel tw-panel">
      <div className="bx-panel-head">
        <span className="bx-eyebrow">⚙ PARÁMETROS</span>
        <span className="tw-demo-badge">SOLO DEMO</span>
      </div>

      <div className="tw-warn">↯ Cambios se aplican en tiempo real</div>

      <div className="tw-body">
        {/* Demo mode toggle */}
        <div className="tw-row">
          <div className="tw-row-head">
            <span className="bx-eyebrow">Demo Mode</span>
            <button
              className={'tw-toggle' + (cfg.demo_mode ? ' active' : '')}
              onClick={() => applyPatch({ demo_mode: !cfg.demo_mode })}
            >
              {cfg.demo_mode ? 'ON' : 'OFF'}
            </button>
          </div>
        </div>

        {/* Fee table */}
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
                              if (!isNaN(v)) {
                                const current = cfg.fees?.[name]
                                if (current) patch({ fees: { [name]: { taker_fee: v, slippage: current.slippage, withdrawal_btc: current.withdrawal_btc, network_latency_bps: current.network_latency_bps } } })
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
                              if (!isNaN(v)) {
                                const current = cfg.fees?.[name]
                                if (current) patch({ fees: { [name]: { taker_fee: current.taker_fee, slippage: v, withdrawal_btc: current.withdrawal_btc, network_latency_bps: current.network_latency_bps } } })
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

        <div className="tw-divider" />

        <SliderRow
          label="Min Net Profit"
          value={cfg.min_net_profit_pct}
          min={0}
          max={0.003}
          step={0.0001}
          display={fmtPct(cfg.min_net_profit_pct)}
          accent="orange"
          onChange={(v) => debouncedPatch({ min_net_profit_pct: v })}
          onCommit={(v) => applyPatch({ min_net_profit_pct: v })}
        />

        <SliderRow
          label="Max Position USDT"
          value={cfg.max_position_usdt}
          min={100}
          max={5000}
          step={100}
          display={'$' + cfg.max_position_usdt.toLocaleString()}
          accent="orange"
          onChange={(v) => debouncedPatch({ max_position_usdt: v })}
          onCommit={(v) => applyPatch({ max_position_usdt: v })}
        />

        <SliderRow
          label="Staleness Threshold"
          value={cfg.staleness_threshold_ms}
          min={500}
          max={10000}
          step={500}
          display={fmtMs(cfg.staleness_threshold_ms)}
          onChange={(v) => debouncedPatch({ staleness_threshold_ms: v })}
          onCommit={(v) => applyPatch({ staleness_threshold_ms: v })}
        />

        <SliderRow
          label="Execution Interval"
          value={cfg.execution_interval_ms}
          min={100}
          max={5000}
          step={100}
          display={fmtMs(cfg.execution_interval_ms)}
          onChange={(v) => debouncedPatch({ execution_interval_ms: v })}
          onCommit={(v) => applyPatch({ execution_interval_ms: v })}
        />

        <div className="tw-section-label">Circuit Breaker</div>

        <StepperRow
          label="Consecutive Losses"
          value={cfg.circuit_breaker_n}
          min={2}
          max={10}
          onChange={(v) => applyPatch({ circuit_breaker_n: v })}
        />

        <SliderRow
          label="Loss Threshold"
          value={cfg.circuit_breaker_loss_pct}
          min={-0.05}
          max={-0.001}
          step={0.001}
          display={fmtPct(cfg.circuit_breaker_loss_pct)}
          accent="down"
          onChange={(v) => debouncedPatch({ circuit_breaker_loss_pct: v })}
          onCommit={(v) => applyPatch({ circuit_breaker_loss_pct: v })}
        />

        <div className="tw-divider" />

        <button
          className="tw-reset"
          onClick={() => {
            patch(DEFAULTS)
              .then((r) => r.json())
              .then((data: Config) => setCfg(data))
          }}
        >
          ↺ Reset a defaults
        </button>
      </div>
    </div>
  )
}
