import { useEffect } from 'react'
import { TrendingUp } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'

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

  label: {
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

export default function StrategyPnL() {
  const strategyPnL = useMarketStore((s) => s.strategyPnL)
  const fetchStrategyPnL = useMarketStore((s) => s.fetchStrategyPnL)
  const trades = useMarketStore((s) => s.trades)

  useEffect(() => {
    fetchStrategyPnL()
    const iv = setInterval(fetchStrategyPnL, 5000)
    return () => clearInterval(iv)
  }, [trades.length, fetchStrategyPnL])

  const maxAbs = strategyPnL.reduce((m, r) => Math.max(m, Math.abs(r.total_pnl)), 0)

  return (
    <div style={styles.panel}>
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <TrendingUp size={13} strokeWidth={1.75} /> P&L by strategy
        </span>
        <span style={styles.meta}>{strategyPnL.length} strategies</span>
      </div>

      <div style={styles.body}>
        {strategyPnL.length === 0 ? (
          <div style={styles.empty}>No trades attributed to a strategy yet.</div>
        ) : (
          strategyPnL.map((r) => {
            const pos = r.total_pnl >= 0
            const widthPct = maxAbs > 0 ? (Math.abs(r.total_pnl) / maxAbs) * 100 : 0
            return (
              <div key={r.strategy} style={styles.row}>
                <span style={styles.label}>{r.strategy}</span>
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
                  {pos ? '+' : '−'}${Math.abs(r.total_pnl).toFixed(2)}
                </span>
                <span style={{ minWidth: 36, textAlign: 'right', color: 'var(--fg-3)' }}>
                  {r.trade_count}
                </span>
                <span style={{ minWidth: 44, textAlign: 'right', color: 'var(--fg-3)' }}>
                  {(r.win_rate * 100).toFixed(0)}%
                </span>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
