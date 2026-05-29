import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { LineChart, Hourglass } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'
import { fmtSigned } from '../utils/format'

const PADL = 40
const PADR = 12
const PADT = 12
const PADB = 20
const CHART_H = 200

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
      top: `${py - 40}px`,
      transform: 'translateX(-50%)',
      background: 'var(--bg-elevated)',
      border: '1px solid var(--line)',
      borderRadius: 'var(--r-sm)',
      padding: '4px 8px',
      pointerEvents: 'none' as const,
      zIndex: 10,
    } as React.CSSProperties),
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
  const positive = last >= 0
  const color = positive ? 'var(--up)' : 'var(--down)'
  const W = Math.max(320, containerWidth)
  const plotW = W - PADL - PADR
  const plotH = CHART_H - PADT - PADB

  let chartBody: React.ReactNode = null
  let tooltip: React.ReactNode = null

  if (hist.length >= 2) {
    const t0 = hist[0].time
    const t1 = hist[hist.length - 1].time
    const vals = hist.map((p) => p.value)
    const vmax = Math.max(0, ...vals)
    const vmin = Math.min(0, ...vals)
    const pad = (vmax - vmin) * 0.12 || 1
    const lo = vmin - pad
    const hi = vmax + pad

    const xOf = (t: number) =>
      PADL + (t1 === t0 ? 0.5 : (t - t0) / (t1 - t0)) * plotW
    const yOf = (v: number) =>
      PADT + (1 - (v - lo) / (hi - lo)) * plotH

    const pts = hist.map((p) => [xOf(p.time), yOf(p.value)] as [number, number])
    const linePath = pts
      .map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1))
      .join(' ')
    const y0 = Math.max(PADT, Math.min(PADT + plotH, yOf(0)))
    const areaPath =
      linePath +
      ` L ${pts[pts.length - 1][0].toFixed(1)} ${y0} L ${pts[0][0].toFixed(1)} ${y0} Z`

    // Y-axis ticks
    const ticks: Array<{ v: number; y: number }> = []
    for (let i = 0; i <= 3; i++) {
      const v = lo + ((hi - lo) * i) / 3
      ticks.push({ v, y: yOf(v) })
    }

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
            <stop offset="0%" stopColor={color} stopOpacity="0.30" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        {ticks.map((t, i) => (
          <g key={i}>
            <line
              x1={PADL}
              y1={t.y}
              x2={W - PADR}
              y2={t.y}
              stroke="var(--line-faint)"
              strokeWidth="1"
            />
            <text style={styles.axis} x={PADL - 6} y={t.y + 3} textAnchor="end">
              {(t.v >= 0 ? '$' : '−$') + Math.abs(Math.round(t.v))}
            </text>
          </g>
        ))}
        <line
          x1={PADL}
          y1={y0}
          x2={W - PADR}
          y2={y0}
          stroke="var(--line)"
          strokeWidth="1"
        />
        <path d={areaPath} fill="url(#pnlg)" />
        <path d={linePath} fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round" />
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

  return (
    <div style={styles.panel} id="bx-pnl">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <LineChart size={13} strokeWidth={1.75} /> P&amp;L acumulado
        </span>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: '12px',
            color,
          }}
        >
          {hist.length ? fmtSigned(last) : '—'}
        </span>
      </div>
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
