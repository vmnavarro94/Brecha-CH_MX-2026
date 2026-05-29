import { useLayoutEffect, useRef, useState } from 'react'
import { Activity, Loader } from 'lucide-react'
import { useMarketStore, FEATURED_PAIRS, PAIR_COLOR } from '../store/marketStore'
import type { SpreadStats } from '../types/api'
import type { ZPoint, TradeMark } from '../store/marketStore'

const PAIR_LABEL: Record<string, string> = {
  'binance-kraken': 'Binance · Kraken',
  'binance-bybit': 'Binance · Bybit',
  'kraken-bybit': 'Kraken · Bybit',
}

const Z_MIN = -3.5
const Z_MAX = 3.5
const WINDOW = 60_000

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
}

export default function SpreadChart() {
  const zSeries = useMarketStore((s) => s.zSeries)
  const spreads = useMarketStore((s) => s.spreads)
  const tradeMarks = useMarketStore((s) => s.tradeMarks)
  const [vis, setVis] = useState<Record<string, boolean>>({
    'binance-kraken': true,
    'binance-bybit': true,
    'kraken-bybit': true,
  })
  const [width, setWidth] = useState(760)
  const bodyRef = useRef<HTMLDivElement>(null)

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
  const now = Date.now()

  const xOf = (t: number) =>
    PADL + Math.max(0, Math.min(1, (t - (now - WINDOW)) / WINDOW)) * plotW
  const yOf = (z: number) =>
    PADT + (1 - (z - Z_MIN) / (Z_MAX - Z_MIN)) * plotH

  const statFor = (p: string): SpreadStats | undefined =>
    spreads.find((s) => s.Pair === p)

  const warming = FEATURED_PAIRS.filter(
    (p) => vis[p] && statFor(p) && (statFor(p)!.Samples < 100)
  )

  function pathFor(pair: string): string {
    const pts: ZPoint[] = zSeries[pair] ?? []
    if (pts.length < 2) return ''
    return pts
      .map((p, i) => (i ? 'L' : 'M') + xOf(p.t).toFixed(1) + ' ' + yOf(p.z).toFixed(1))
      .join(' ')
  }

  return (
    <div style={panelStyles.panel} id="bx-zscore">
      <div style={panelStyles.head}>
        <span style={panelStyles.eyebrow}>
          <Activity size={13} strokeWidth={1.75} /> Z-score del spread · ventana 60 s
        </span>
        <span style={panelStyles.meta}>(spread − μ) / σ · por par</span>
      </div>

      <div style={panelStyles.body} ref={bodyRef}>
        {warming.length > 0 && (() => {
          const stat = statFor(warming[0])!
          return (
            <div style={panelStyles.warming}>
              <Loader size={12} strokeWidth={1.75} />
              Calentando modelo: {stat.Samples}/100 samples
              <span style={panelStyles.warningBar}>
                <span style={panelStyles.warningBarFill(Math.min(100, stat.Samples))} />
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
          {FEATURED_PAIRS.map((pair) => {
            if (!vis[pair]) return null
            const stat = statFor(pair)
            return (
              <path
                key={pair}
                d={pathFor(pair)}
                fill="none"
                stroke={PAIR_COLOR[pair]}
                strokeWidth="1.6"
                strokeLinejoin="round"
                strokeLinecap="round"
                opacity={stat && stat.Samples < 100 ? 0.4 : 1}
              />
            )
          })}

          {/* End-of-series dots */}
          {FEATURED_PAIRS.map((pair) => {
            const arr = zSeries[pair]
            if (!vis[pair] || !arr || !arr.length) return null
            const last = arr[arr.length - 1]
            return (
              <circle
                key={pair}
                cx={xOf(last.t)}
                cy={yOf(last.z)}
                r="3"
                fill={PAIR_COLOR[pair]}
              />
            )
          })}

          {/* Trade markers */}
          {FEATURED_PAIRS.map((pair) => {
            if (!vis[pair]) return null
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
        {FEATURED_PAIRS.map((pair) => {
          const stat = statFor(pair)
          const arr = zSeries[pair]
          const z = arr && arr.length ? arr[arr.length - 1].z : 0
          const hot = Math.abs(z) > 2
          return (
            <div
              key={pair}
              style={panelStyles.legItem(vis[pair])}
              onClick={() => setVis((v) => ({ ...v, [pair]: !v[pair] }))}
            >
              <span style={panelStyles.legSwatch(PAIR_COLOR[pair])} />
              <span style={panelStyles.legLabel}>{PAIR_LABEL[pair]}</span>
              <span style={panelStyles.legZ(hot)}>
                z {z >= 0 ? '+' : '−'}{Math.abs(z).toFixed(2)}
              </span>
              {stat && stat.Samples < 100 && (
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: '11px', color: 'var(--warn)' }}>
                  · {stat.Samples}/100
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
