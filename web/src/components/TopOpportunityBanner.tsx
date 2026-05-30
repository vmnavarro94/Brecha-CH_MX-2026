import { useEffect, useState } from 'react'
import { Zap, ArrowRight } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'

const RECENT_MS = 5_000

const styles = {
  wrap: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 'var(--sp-4)',
    padding: '10px 16px',
    margin: '0 0 14px 0',
    background: 'var(--bg-surface)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-lg)',
    minHeight: '54px',
    flexWrap: 'nowrap' as const,
    overflow: 'hidden',
  } as React.CSSProperties,

  left: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--sp-3)',
  } as React.CSSProperties,

  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '5px',
    padding: '3px 8px',
    borderRadius: 'var(--r-pill)',
    background: 'var(--orange-wash)',
    color: 'var(--orange)',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 600,
    letterSpacing: '0.08em',
    textTransform: 'uppercase' as const,
  } as React.CSSProperties,

  pair: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '6px',
    fontFamily: 'var(--font-sans)',
    fontSize: '14px',
    fontWeight: 600,
    color: 'var(--fg-1)',
    textTransform: 'capitalize' as const,
  } as React.CSSProperties,

  right: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--sp-4)',
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
  } as React.CSSProperties,

  metric: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '1px',
  } as React.CSSProperties,

  metricKey: {
    fontFamily: 'var(--font-mono)',
    fontSize: '9px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
  } as React.CSSProperties,

  metricVal: (color: string) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '14px',
      fontWeight: 600,
      color,
    } as React.CSSProperties),

  ago: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  empty: {
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
    padding: '10px 16px',
    margin: '0 0 14px 0',
    background: 'var(--bg-surface)',
    border: '1px dashed var(--line)',
    borderRadius: 'var(--r-lg)',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
    letterSpacing: '0.04em',
    minHeight: '54px',
  } as React.CSSProperties,
}

export default function TopOpportunityBanner() {
  const opportunities = useMarketStore((s) => s.opportunities)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(iv)
  }, [])

  const recent = opportunities.filter((o) => now - (o._t ?? 0) < RECENT_MS)

  if (recent.length === 0) {
    return (
      <div style={styles.empty}>
        <Zap size={12} strokeWidth={1.75} />
        Motor escuchando · sin oportunidad activa
      </div>
    )
  }

  const top = recent.reduce((best, o) => (o.Score > best.Score ? o : best), recent[0])
  const ageMs = now - (top._t ?? now)
  const ageStr = ageMs < 1000 ? `${ageMs}ms` : `${(ageMs / 1000).toFixed(1)}s`

  const zHot = Math.abs(top.ZScore) > 2

  return (
    <div style={styles.wrap}>
      <div style={styles.left}>
        <span style={styles.badge}>
          <Zap size={11} strokeWidth={2} /> Top oportunidad
        </span>
        <span style={styles.pair}>
          {top.BuyExchange}
          <ArrowRight size={14} strokeWidth={1.75} style={{ color: 'var(--orange)' }} />
          {top.SellExchange}
        </span>
        <span style={styles.ago}>hace {ageStr}</span>
      </div>

      <div style={styles.right}>
        <div style={styles.metric}>
          <span style={styles.metricKey}>Net %</span>
          <span style={styles.metricVal('var(--up)')}>
            +{(top.NetProfitPct * 100).toFixed(4)}%
          </span>
        </div>
        <div style={styles.metric}>
          <span style={styles.metricKey}>Z-score</span>
          <span style={styles.metricVal(zHot ? 'var(--orange)' : 'var(--fg-1)')}>
            {top.ZScore >= 0 ? '+' : ''}{top.ZScore.toFixed(2)}
          </span>
        </div>
        <div style={styles.metric}>
          <span style={styles.metricKey}>Score</span>
          <span style={styles.metricVal('var(--orange)')}>{top.Score.toFixed(3)}</span>
        </div>
        <div style={styles.metric}>
          <span style={styles.metricKey}>Estado</span>
          <span style={styles.metricVal('var(--info)')}>{top.Status}</span>
        </div>
      </div>
    </div>
  )
}
