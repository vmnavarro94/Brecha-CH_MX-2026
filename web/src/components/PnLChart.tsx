import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { LineChart, Hourglass } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'
import { fmtSigned } from '../utils/format'

const PADL = 48
const PADR = 12
const PADT = 14
const PADB = 22
const CHART_H = 220

const styles = {
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

  headerStats: {
    display: 'flex',
    alignItems: 'baseline',
    gap: '18px',
  } as React.CSSProperties,

  bigVal: (color: string) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '15px',
      fontWeight: 600,
      color,
    } as React.CSSProperties),

  delta: (color: string) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '11px',
      color,
    } as React.CSSProperties),

  statsRow: {
    display: 'flex',
    gap: '20px',
    padding: '6px 15px',
    borderBottom: '1px solid var(--line-faint)',
    background: 'var(--bg-inset)',
  } as React.CSSProperties,

  statItem: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '1px',
  } as React.CSSProperties,

  statLabel: {
    fontFamily: 'var(--font-mono)',
    fontSize: '9px',
    color: 'var(--fg-3)',
    letterSpacing: '0.08em',
    textTransform: 'uppercase' as const,
  } as React.CSSProperties,

  statVal: (color: string) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '12px',
      color,
    } as React.CSSProperties),

  body: {
    position: 'relative' as const,
    height: `${CHART_H}px`,
    overflow: 'hidden',
  } as React.CSSProperties,

  empty: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: '8px',
    height: '100%',
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  axis: {
    fontFamily: 'var(--font-mono)',
    fontSize: '9px',
    fill: 'var(--fg-3)',
  } as React.CSSProperties,

  tip: (px: number, py: number) =>
    ({
      position: 'absolute' as const,
      left: `${px}px`,
      top: `${py - 46}px`,
      transform: 'translateX(-50%)',
      background: 'var(--bg-elevated)',
      border: '1px solid var(--line)',
      borderRadius: 'var(--r-sm)',
      padding: '5px 9px',
      pointerEvents: 'none' as const,
      zIndex: 10,
      whiteSpace: 'nowrap' as const,
    } as React.CSSProperties),
}

function fmtCompact(v: number): string {
  const abs = Math.abs(v)
  const sign = v < 0 ? '−' : ''
  if (abs >= 1000) return sign + '$' + (abs / 1000).toFixed(abs >= 10000 ? 1 : 2) + 'k'
  return sign + '$' + abs.toFixed(0)
}

function fmtAxisTime(ms: number): string {
  return new Date(ms).toLocaleTimeString('es-MX', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export default function PnLChart() {
  const histRaw = useMarketStore((s) => s.pnlHistory)
  const hist = useMemo(
    () => [...histRaw].sort((a, b) => a.time - b.time),
    [histRaw]
  )
  const [containerWidth, setContainerWidth] = useState(700)
  const [hover, setHover] = useState<{ i: number; px: number; py: number } | null>(null)
  const bodyRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const el = bodyRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setContainerWidth(el.clientWidth))
    ro.observe(el)
    setContainerWidth(el.clientWidth)
    return () => ro.disconnect()
  }, [])

  const last = hist.length ? hist[hist.length - 1].value : 0
  const first = hist.length ? hist[0].value : 0
  const positive = last >= 0
  const color = positive ? 'var(--up)' : 'var(--down)'
  const W = Math.max(320, containerWidth)
  const plotW = W - PADL - PADR
  const plotH = CHART_H - PADT - PADB

  // Derived stats — always defined for header display.
  const peak = hist.length ? Math.max(...hist.map((p) => p.value)) : 0
  const trough = hist.length ? Math.min(...hist.map((p) => p.value)) : 0
  const hourAgo = Date.now() - 3600_000
  const lastHourBase = hist.find((p) => p.time >= hourAgo)?.value ?? first
  const hourDelta = last - lastHourBase
  const totalDelta = last - first

  let chartBody: React.ReactNode = null
  let tooltip: React.ReactNode = null

  if (hist.length >= 2) {
    const t0 = hist[0].time
    const t1 = hist[hist.length - 1].time

    // Dynamic Y-range with padding — don't force 0 into the axis. PnL that grows
    // monotonically from 0 to $10k should fill the plot, not flatten against the
    // top with $0 anchored at the bottom.
    const dataMin = Math.min(...hist.map((p) => p.value))
    const dataMax = Math.max(...hist.map((p) => p.value))
    const range = Math.max(1, dataMax - dataMin)
    const pad = range * 0.1
    const lo = dataMin - pad
    const hi = dataMax + pad

    const xOf = (t: number) =>
      PADL + (t1 === t0 ? 0.5 : (t - t0) / (t1 - t0)) * plotW
    const yOf = (v: number) =>
      PADT + (1 - (v - lo) / (hi - lo)) * plotH

    const pts = hist.map((p) => [xOf(p.time), yOf(p.value)] as [number, number])
    const linePath = pts
      .map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1))
      .join(' ')

    // Area drops to the bottom of the plot, not to y(0). Gives a clean fill
    // under the line that scales with the actual data range.
    const plotBottom = PADT + plotH
    const areaPath =
      linePath +
      ` L ${pts[pts.length - 1][0].toFixed(1)} ${plotBottom} L ${pts[0][0].toFixed(1)} ${plotBottom} Z`

    // Y-axis ticks — 4 evenly spaced across the actual range.
    const ticks: Array<{ v: number; y: number }> = []
    for (let i = 0; i <= 3; i++) {
      const v = lo + ((hi - lo) * i) / 3
      ticks.push({ v, y: yOf(v) })
    }

    // X-axis ticks — start, middle, end.
    const xTicks = [
      { t: t0, x: xOf(t0) },
      { t: t0 + (t1 - t0) / 2, x: xOf(t0 + (t1 - t0) / 2) },
      { t: t1, x: xOf(t1) },
    ]

    // Zero reference line — only render if 0 falls inside the dynamic range.
    const showZero = lo <= 0 && hi >= 0
    const yZero = showZero ? yOf(0) : 0

    chartBody = (
      <svg
        width={W}
        height={CHART_H}
        style={{ display: 'block' }}
        onMouseMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect()
          const mx = e.clientX - rect.left
          let best = 0
          let bd = Infinity
          pts.forEach((p, i) => {
            const d = Math.abs(p[0] - mx)
            if (d < bd) { bd = d; best = i }
          })
          setHover({ i: best, px: pts[best][0], py: pts[best][1] })
        }}
        onMouseLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id="pnlg" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.35" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        {ticks.map((t, i) => (
          <g key={`y${i}`}>
            <line
              x1={PADL}
              y1={t.y}
              x2={W - PADR}
              y2={t.y}
              stroke="var(--line-faint)"
              strokeWidth="1"
            />
            <text style={styles.axis} x={PADL - 6} y={t.y + 3} textAnchor="end">
              {fmtCompact(t.v)}
            </text>
          </g>
        ))}
        {xTicks.map((t, i) => (
          <text
            key={`x${i}`}
            style={styles.axis}
            x={t.x}
            y={PADT + plotH + 14}
            textAnchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
          >
            {fmtAxisTime(t.t)}
          </text>
        ))}
        {showZero && (
          <line
            x1={PADL}
            y1={yZero}
            x2={W - PADR}
            y2={yZero}
            stroke="var(--line)"
            strokeWidth="1"
            strokeDasharray="3 3"
          />
        )}
        <path d={areaPath} fill="url(#pnlg)" />
        <path d={linePath} fill="none" stroke={color} strokeWidth="1.8" strokeLinejoin="round" />
        {/* Peak + trough markers */}
        {hist.length > 2 && (() => {
          const peakIdx = hist.findIndex((p) => p.value === peak)
          const troughIdx = hist.findIndex((p) => p.value === trough)
          return (
            <>
              {peakIdx >= 0 && peakIdx !== hist.length - 1 && (
                <g>
                  <circle cx={pts[peakIdx][0]} cy={pts[peakIdx][1]} r="3" fill="var(--up)" />
                  <text
                    style={{ ...styles.axis, fill: 'var(--up)', fontSize: '9px' }}
                    x={pts[peakIdx][0]}
                    y={pts[peakIdx][1] - 6}
                    textAnchor="middle"
                  >
                    pico
                  </text>
                </g>
              )}
              {troughIdx >= 0 && trough < 0 && (
                <g>
                  <circle cx={pts[troughIdx][0]} cy={pts[troughIdx][1]} r="3" fill="var(--down)" />
                  <text
                    style={{ ...styles.axis, fill: 'var(--down)', fontSize: '9px' }}
                    x={pts[troughIdx][0]}
                    y={pts[troughIdx][1] + 14}
                    textAnchor="middle"
                  >
                    fondo
                  </text>
                </g>
              )}
            </>
          )
        })()}
        {/* Last-value dot */}
        {pts.length > 0 && (
          <circle
            cx={pts[pts.length - 1][0]}
            cy={pts[pts.length - 1][1]}
            r="4"
            fill={color}
            stroke="var(--bg-surface)"
            strokeWidth="1.5"
          />
        )}
        {hover && (
          <g>
            <line
              x1={hover.px}
              y1={PADT}
              x2={hover.px}
              y2={PADT + plotH}
              stroke="var(--line-strong)"
              strokeWidth="1"
              strokeDasharray="2 3"
            />
            <circle
              cx={hover.px}
              cy={hover.py}
              r="4"
              fill={color}
              stroke="var(--bg-surface)"
              strokeWidth="1.5"
            />
          </g>
        )}
      </svg>
    )

    if (hover) {
      const p = hist[hover.i]
      tooltip = (
        <div style={styles.tip(hover.px, hover.py)}>
          <div
            style={{
              fontFamily: 'var(--font-mono)',
              fontVariantNumeric: 'tabular-nums',
              fontSize: '12px',
              fontWeight: 600,
              color: p.value >= 0 ? 'var(--up)' : 'var(--down)',
            }}
          >
            {fmtSigned(p.value)}
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: '10px', color: 'var(--fg-3)' }}>
            {new Date(p.time).toLocaleTimeString('es-MX', {
              hour: '2-digit',
              minute: '2-digit',
              second: '2-digit',
              hour12: false,
            })}
          </div>
        </div>
      )
    }
  }

  const hourColor = hourDelta >= 0 ? 'var(--up)' : 'var(--down)'

  return (
    <div style={styles.panel} id="bx-pnl">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <LineChart size={13} strokeWidth={1.75} /> P&amp;L acumulado
        </span>
        <span style={styles.headerStats}>
          <span style={styles.bigVal(color)}>
            {hist.length ? fmtSigned(last) : '—'}
          </span>
          {hist.length >= 2 && (
            <span style={styles.delta(hourColor)}>
              {hourDelta >= 0 ? '↑' : '↓'} {fmtSigned(Math.abs(hourDelta))} · 1h
            </span>
          )}
        </span>
      </div>
      {hist.length >= 2 && (
        <div style={styles.statsRow}>
          <div style={styles.statItem}>
            <span style={styles.statLabel}>Pico</span>
            <span style={styles.statVal('var(--up)')}>{fmtSigned(peak)}</span>
          </div>
          <div style={styles.statItem}>
            <span style={styles.statLabel}>Fondo</span>
            <span style={styles.statVal(trough < 0 ? 'var(--down)' : 'var(--fg-2)')}>
              {fmtSigned(trough)}
            </span>
          </div>
          <div style={styles.statItem}>
            <span style={styles.statLabel}>Δ sesión</span>
            <span style={styles.statVal(totalDelta >= 0 ? 'var(--up)' : 'var(--down)')}>
              {fmtSigned(totalDelta)}
            </span>
          </div>
          <div style={styles.statItem}>
            <span style={styles.statLabel}>Trades</span>
            <span style={styles.statVal('var(--fg-2)')}>{hist.length}</span>
          </div>
        </div>
      )}
      <div style={styles.body} ref={bodyRef}>
        {hist.length < 2 ? (
          <div style={styles.empty}>
            <Hourglass size={20} strokeWidth={1.75} />
            Esperando primer trade…
          </div>
        ) : (
          chartBody
        )}
        {tooltip}
      </div>
    </div>
  )
}
