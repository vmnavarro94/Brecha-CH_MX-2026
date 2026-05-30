import { useEffect, useState } from 'react'
import { BarChart3 } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'

interface PairStat {
  pair: string
  total_pnl: number
  trade_count: number
  win_rate: number
  total_volume: number
}

const styles = {
  panel: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-lg)',
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column' as const,
    height: '100%',
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
    overflowY: 'auto' as const,
    flex: 1,
    padding: '4px 0',
  } as React.CSSProperties,

  row: {
    display: 'flex',
    alignItems: 'center',
    gap: '10px',
    padding: '8px 14px',
    borderBottom: '1px solid var(--line-faint)',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
  } as React.CSSProperties,

  pair: {
    flex: 1,
    color: 'var(--fg-1)',
    textTransform: 'capitalize' as const,
    fontVariantNumeric: 'tabular-nums',
  } as React.CSSProperties,

  bar: {
    flex: 1.5,
    height: '4px',
    background: 'var(--bg-inset)',
    borderRadius: 'var(--r-pill)',
    overflow: 'hidden',
    position: 'relative' as const,
  } as React.CSSProperties,

  empty: {
    padding: '24px 14px',
    color: 'var(--fg-3)',
    textAlign: 'center' as const,
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
  } as React.CSSProperties,
}

export default function PerPairPnL() {
  const trades = useMarketStore((s) => s.trades)
  const [pairs, setPairs] = useState<PairStat[]>([])

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const r = await fetch('/api/pnl-by-pair')
        if (!r.ok) return
        const body = await r.json()
        if (!cancelled && Array.isArray(body.pairs)) setPairs(body.pairs)
      } catch {
        // silent — polled again
      }
    }
    load()
    const iv = setInterval(load, 5000)
    return () => {
      cancelled = true
      clearInterval(iv)
    }
  }, [trades.length])

  const maxAbs = pairs.reduce((m, p) => Math.max(m, Math.abs(p.total_pnl)), 0)

  return (
    <div style={styles.panel}>
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <BarChart3 size={13} strokeWidth={1.75} /> P&L por par
        </span>
        <span style={styles.meta}>{pairs.length} pares · top 8</span>
      </div>

      <div style={styles.body}>
        {pairs.length === 0 ? (
          <div style={styles.empty}>Sin trades aún en esta sesión.</div>
        ) : (
          pairs.slice(0, 8).map((p) => {
            const pos = p.total_pnl >= 0
            const widthPct = maxAbs > 0 ? (Math.abs(p.total_pnl) / maxAbs) * 100 : 0
            return (
              <div key={p.pair} style={styles.row}>
                <span style={styles.pair}>{p.pair.replace('->', ' → ')}</span>
                <span style={styles.bar}>
                  <span
                    style={{
                      position: 'absolute',
                      left: 0,
                      top: 0,
                      bottom: 0,
                      width: `${widthPct}%`,
                      background: pos ? 'var(--up)' : 'var(--down)',
                    }}
                  />
                </span>
                <span
                  style={{
                    minWidth: 70,
                    textAlign: 'right',
                    color: pos ? 'var(--up)' : 'var(--down)',
                    fontWeight: 600,
                  }}
                >
                  {pos ? '+' : '−'}${Math.abs(p.total_pnl).toFixed(2)}
                </span>
                <span style={{ minWidth: 36, textAlign: 'right', color: 'var(--fg-3)' }}>
                  {p.trade_count}
                </span>
                <span style={{ minWidth: 44, textAlign: 'right', color: 'var(--fg-3)' }}>
                  {(p.win_rate * 100).toFixed(0)}%
                </span>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
