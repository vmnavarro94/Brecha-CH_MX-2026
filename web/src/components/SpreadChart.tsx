import { useLayoutEffect, useRef, useState, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { Activity, Loader, Settings2, RotateCcw } from 'lucide-react'
import { useMarketStore, pairColor, MAX_FEATURED } from '../store/marketStore'
import type { SpreadStats } from '../types/api'
import type { ZPoint, TradeMark } from '../store/marketStore'

function pairLabel(pair: string): string {
  return pair.split('-').map((e) => e.charAt(0).toUpperCase() + e.slice(1)).join(' · ')
}

const Z_MIN = -3.5
const Z_MAX = 3.5
const WINDOW = 60_000
const MIN_SAMPLES = 30

const PADL = 30
const PADR = 14
const PADT = 14
const PADB = 18
const CHART_H = 250

const refLines = [
  { z: 2, c: 'var(--orange-line)', dash: '3 4', lbl: '+2σ' },
  { z: 1, c: 'rgba(255,197,61,0.28)', dash: '2 5', lbl: '+1σ' },
  { z: 0, c: 'var(--line)', dash: '', lbl: '0' },
  { z: -1, c: 'rgba(255,197,61,0.28)', dash: '2 5', lbl: '−1σ' },
  { z: -2, c: 'var(--orange-line)', dash: '3 4', lbl: '−2σ' },
]

const panelStyles = {
  panel: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-lg)',
    overflow: 'hidden',
  } as React.CSSProperties,

  head: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 15px',
    borderBottom: '1px solid var(--line-faint)',
  } as React.CSSProperties,

  eyebrow: {
    display: 'flex',
    alignItems: 'center',
    gap: '5px',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 500,
    letterSpacing: '0.09em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  meta: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  body: {
    position: 'relative' as const,
    padding: '0',
    overflowX: 'hidden' as const,
  } as React.CSSProperties,

  warming: {
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
    padding: '6px 14px',
    background: 'rgba(255,197,61,0.08)',
    borderBottom: '1px solid rgba(255,197,61,0.2)',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--warn)',
  } as React.CSSProperties,

  warningBar: {
    display: 'inline-block',
    width: '60px',
    height: '4px',
    background: 'var(--line)',
    borderRadius: 'var(--r-pill)',
    overflow: 'hidden',
    verticalAlign: 'middle',
  } as React.CSSProperties,

  warningBarFill: (pct: number) =>
    ({
      display: 'block',
      height: '100%',
      width: `${pct}%`,
      background: 'var(--warn)',
      borderRadius: 'var(--r-pill)',
    } as React.CSSProperties),

  svgAxis: {
    fontFamily: 'var(--font-mono)',
    fontSize: '9px',
    fill: 'var(--fg-3)',
  } as React.CSSProperties,

  legend: {
    display: 'flex',
    alignItems: 'center',
    gap: '14px',
    padding: '8px 14px',
    borderTop: '1px solid var(--line-faint)',
    flexWrap: 'wrap' as const,
  } as React.CSSProperties,

  legItem: (on: boolean) =>
    ({
      display: 'flex',
      alignItems: 'center',
      gap: '5px',
      cursor: 'pointer',
      opacity: on ? 1 : 0.35,
      userSelect: 'none' as const,
    } as React.CSSProperties),

  legSwatch: (color: string) =>
    ({
      width: '10px',
      height: '10px',
      borderRadius: '2px',
      background: color,
      flexShrink: 0,
    } as React.CSSProperties),

  legLabel: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  legZ: (hot: boolean) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontSize: '11px',
      color: hot ? 'var(--orange)' : 'var(--fg-3)',
    } as React.CSSProperties),

  toolBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    padding: '4px 8px',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-sm)',
    background: 'transparent',
    color: 'var(--fg-2)',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    letterSpacing: '0.06em',
    textTransform: 'uppercase' as const,
    cursor: 'pointer',
  } as React.CSSProperties,

  toolBar: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
  } as React.CSSProperties,

  pickerOverlay: {
    position: 'fixed' as const,
    inset: 0,
    background: 'transparent',
    zIndex: 1000,
  } as React.CSSProperties,

  picker: (top: number, right: number) =>
    ({
      position: 'fixed' as const,
      top: `${top}px`,
      right: `${right}px`,
      width: '280px',
      maxHeight: '420px',
      overflowY: 'auto' as const,
      padding: '8px',
      background: 'var(--bg-surface)',
      border: '1px solid var(--line)',
      borderRadius: 'var(--r-md)',
      boxShadow: '0 12px 32px rgba(0,0,0,0.5)',
      zIndex: 1001,
    } as React.CSSProperties),

  pickerHead: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '4px 6px 8px',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
  } as React.CSSProperties,

  pickerRow: (selected: boolean, disabled: boolean) =>
    ({
      display: 'flex',
      alignItems: 'center',
      gap: '8px',
      padding: '5px 6px',
      borderRadius: 'var(--r-sm)',
      cursor: disabled ? 'not-allowed' : 'pointer',
      opacity: disabled ? 0.4 : 1,
      background: selected ? 'rgba(255,197,61,0.08)' : 'transparent',
      fontFamily: 'var(--font-mono)',
      fontSize: '11px',
      color: 'var(--fg-2)',
    } as React.CSSProperties),

  pickerCheck: (selected: boolean) =>
    ({
      width: '10px',
      height: '10px',
      borderRadius: '2px',
      border: '1px solid var(--line)',
      background: selected ? 'var(--orange)' : 'transparent',
      flexShrink: 0,
    } as React.CSSProperties),

  pickerStd: {
    marginLeft: 'auto',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  pickerHint: {
    padding: '6px',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    textAlign: 'center' as const,
  } as React.CSSProperties,
}

export default function SpreadChart() {
  const zSeries = useMarketStore((s) => s.zSeries)
  const spreads = useMarketStore((s) => s.spreads)
  const tradeMarks = useMarketStore((s) => s.tradeMarks)
  const featuredPairs = useMarketStore((s) => s.featuredPairs)
  const selectedPairs = useMarketStore((s) => s.selectedPairs)
  const setSelectedPairs = useMarketStore((s) => s.setSelectedPairs)
  const [vis, setVis] = useState<Record<string, boolean>>({})
  const [width, setWidth] = useState(760)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [pickerAnchor, setPickerAnchor] = useState<{ top: number; right: number } | null>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const pickerBtnRef = useRef<HTMLButtonElement>(null)
  const [now, setNow] = useState(Date.now())

  function openPicker() {
    if (pickerBtnRef.current) {
      const rect = pickerBtnRef.current.getBoundingClientRect()
      setPickerAnchor({
        top: rect.bottom + 6,
        right: Math.max(8, window.innerWidth - rect.right),
      })
    }
    setPickerOpen(true)
  }

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 100)
    return () => clearInterval(id)
  }, [])

  useLayoutEffect(() => {
    const el = bodyRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setWidth(el.clientWidth))
    ro.observe(el)
    setWidth(el.clientWidth)
    return () => ro.disconnect()
  }, [])

  const W = Math.max(320, width)
  const plotW = W - PADL - PADR
  const plotH = CHART_H - PADT - PADB

  const xOf = (t: number) =>
    PADL + Math.max(0, Math.min(1, (t - (now - WINDOW)) / WINDOW)) * plotW
  const yOf = (z: number) =>
    PADT + (1 - (z - Z_MIN) / (Z_MAX - Z_MIN)) * plotH

  const statFor = (p: string): SpreadStats | undefined =>
    spreads.find((s) => s.Pair === p)

  const warming = featuredPairs.filter(
    (p) => vis[p] !== false && statFor(p) && (statFor(p)!.Samples < MIN_SAMPLES)
  )

  function pathFor(pair: string): string {
    const pts: ZPoint[] = zSeries[pair] ?? []
    if (pts.length < 2) return ''
    return pts
      .map((p, i) => (i ? 'L' : 'M') + xOf(p.t).toFixed(1) + ' ' + yOf(p.z).toFixed(1))
      .join(' ')
  }

  const togglePicked = (p: string) => {
    const current = selectedPairs ?? featuredPairs
    if (current.includes(p)) {
      const next = current.filter((x) => x !== p)
      setSelectedPairs(next.length > 0 ? next : null)
    } else {
      if (current.length >= MAX_FEATURED) return
      setSelectedPairs([...current, p])
    }
  }

  // Sort alphabetically by pair name so the picker list does not reshuffle
  // every time Welford nudges Std on a tick. Std is shown next to each row for
  // information, but it does not drive the order.
  const pairsSorted = [...spreads]
    .filter((s) => s.Std > 0)
    .sort((a, b) => a.Pair.localeCompare(b.Pair))

  return (
    <div style={panelStyles.panel} id="bx-zscore">
      <div style={panelStyles.head}>
        <span style={panelStyles.eyebrow}>
          <Activity size={13} strokeWidth={1.75} /> Z-score del spread · ventana 60 s
        </span>
        <span style={panelStyles.toolBar}>
          <span style={panelStyles.meta}>(spread − μ) / σ · por par</span>
          <button
            ref={pickerBtnRef}
            type="button"
            style={panelStyles.toolBtn}
            onClick={() => (pickerOpen ? setPickerOpen(false) : openPicker())}
            aria-label="Elegir pares"
            data-testid="pair-picker-toggle"
          >
            <Settings2 size={11} strokeWidth={1.75} /> Pares ({featuredPairs.length})
          </button>
          {selectedPairs !== null && (
            <button
              type="button"
              style={panelStyles.toolBtn}
              onClick={() => setSelectedPairs(null)}
              aria-label="Volver a auto"
              data-testid="pair-picker-reset"
            >
              <RotateCcw size={11} strokeWidth={1.75} /> Auto
            </button>
          )}
        </span>
      </div>

      {pickerOpen && pickerAnchor && createPortal(
        <>
          <div
            style={panelStyles.pickerOverlay}
            onClick={() => setPickerOpen(false)}
          />
          <div
            style={panelStyles.picker(pickerAnchor.top, pickerAnchor.right)}
            data-testid="pair-picker"
            onClick={(e) => e.stopPropagation()}
          >
            <div style={panelStyles.pickerHead}>
              <span>
                {selectedPairs !== null ? 'Selección manual' : 'Auto (top por σ)'}
              </span>
              <span>{featuredPairs.length}/{MAX_FEATURED}</span>
            </div>
            {pairsSorted.length === 0 && (
              <div style={panelStyles.pickerHint}>
                Esperando spreads con muestras suficientes…
              </div>
            )}
            {pairsSorted.map((s) => {
              const isOn = featuredPairs.includes(s.Pair)
              const atCap = !isOn && featuredPairs.length >= MAX_FEATURED
              // Only show real color for pairs already in the featured set; for
              // candidates outside the set, show a neutral dot so collisions in
              // the hash preview do not visually duplicate colors.
              const swatchColor = isOn
                ? pairColor(s.Pair, featuredPairs)
                : 'var(--line-strong)'
              return (
                <div
                  key={s.Pair}
                  style={panelStyles.pickerRow(isOn, atCap)}
                  onClick={() => !atCap && togglePicked(s.Pair)}
                  data-testid={`pair-row-${s.Pair}`}
                >
                  <span style={panelStyles.pickerCheck(isOn)} />
                  <span style={panelStyles.legSwatch(swatchColor)} />
                  <span>{pairLabel(s.Pair)}</span>
                  <span style={panelStyles.pickerStd}>σ {(s.Std * 1e4).toFixed(2)} bp</span>
                </div>
              )
            })}
          </div>
        </>,
        document.body,
      )}

      <div style={panelStyles.body} ref={bodyRef}>
        {warming.length > 0 && (() => {
          const stat = statFor(warming[0])!
          return (
            <div style={panelStyles.warming}>
              <Loader size={12} strokeWidth={1.75} />
              Calentando modelo: {stat.Samples}/{MIN_SAMPLES} samples
              <span style={panelStyles.warningBar}>
                <span style={panelStyles.warningBarFill(Math.min(100, (stat.Samples / MIN_SAMPLES) * 100))} />
              </span>
            </div>
          )
        })()}

        <svg width={W} height={CHART_H} style={{ display: 'block' }}>
          {/* Reference lines */}
          {refLines.map((r) => (
            <g key={r.z}>
              <line
                x1={PADL}
                y1={yOf(r.z)}
                x2={W - PADR}
                y2={yOf(r.z)}
                stroke={r.c}
                strokeWidth="1"
                strokeDasharray={r.dash}
              />
              <text
                style={panelStyles.svgAxis}
                x={PADL - 6}
                y={yOf(r.z) + 3}
                textAnchor="end"
              >
                {r.lbl}
              </text>
            </g>
          ))}

          {/* Anomaly tint bands */}
          <rect
            x={PADL}
            y={yOf(Z_MAX)}
            width={plotW}
            height={yOf(2) - yOf(Z_MAX)}
            fill="var(--orange-wash)"
            opacity="0.4"
          />
          <rect
            x={PADL}
            y={yOf(-2)}
            width={plotW}
            height={yOf(Z_MIN) - yOf(-2)}
            fill="var(--orange-wash)"
            opacity="0.4"
          />

          {/* Series lines */}
          {featuredPairs.map((pair) => {
            if (vis[pair] === false) return null
            const stat = statFor(pair)
            return (
              <path
                key={pair}
                d={pathFor(pair)}
                fill="none"
                stroke={pairColor(pair, featuredPairs)}
                strokeWidth="1.6"
                strokeLinejoin="round"
                strokeLinecap="round"
                opacity={stat && stat.Samples < MIN_SAMPLES ? 0.4 : 1}
              />
            )
          })}

          {/* End-of-series dots */}
          {featuredPairs.map((pair) => {
            const arr = zSeries[pair]
            if (vis[pair] === false || !arr?.length) return null
            const last = arr[arr.length - 1]
            return (
              <circle
                key={pair}
                cx={xOf(last.t)}
                cy={yOf(last.z)}
                r="3"
                fill={pairColor(pair, featuredPairs)}
              />
            )
          })}

          {/* Trade markers */}
          {featuredPairs.map((pair) => {
            if (vis[pair] === false) return null
            const marks: TradeMark[] = tradeMarks[pair] ?? []
            return marks
              .filter((m) => now - m.t < WINDOW)
              .map((m, i) => (
                <g
                  key={pair + i}
                  data-testid="trade-marker"
                >
                  <circle
                    cx={xOf(m.t)}
                    cy={yOf(m.z)}
                    r="6"
                    fill="none"
                    stroke={m.profit >= 0 ? 'var(--up)' : 'var(--down)'}
                    strokeWidth="1.4"
                    opacity="0.9"
                  />
                  <circle
                    cx={xOf(m.t)}
                    cy={yOf(m.z)}
                    r="2.4"
                    fill={m.profit >= 0 ? 'var(--up)' : 'var(--down)'}
                  />
                </g>
              ))
          })}
        </svg>
      </div>

      {/* Legend */}
      <div style={panelStyles.legend}>
        {featuredPairs.map((pair) => {
          const stat = statFor(pair)
          const arr = zSeries[pair]
          const z = arr && arr.length ? arr[arr.length - 1].z : 0
          const hot = Math.abs(z) > 2
          return (
            <div
              key={pair}
              style={panelStyles.legItem(vis[pair] !== false)}
              onClick={() => setVis((v) => ({ ...v, [pair]: !v[pair] }))}
            >
              <span style={panelStyles.legSwatch(pairColor(pair, featuredPairs))} />
              <span style={panelStyles.legLabel}>{pairLabel(pair)}</span>
              <span style={panelStyles.legZ(hot)}>
                z {z >= 0 ? '+' : '−'}{Math.abs(z).toFixed(2)}
              </span>
              {stat && stat.Samples < MIN_SAMPLES && (
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: '11px', color: 'var(--warn)' }}>
                  · {stat.Samples}/{MIN_SAMPLES}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
