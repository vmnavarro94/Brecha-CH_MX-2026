import { TrendingUp } from 'lucide-react'
import { useMarketStore, EXCHANGES } from '../store/marketStore'
import { fmtSigned } from '../utils/format'

const CB_MAP = {
  active: {
    label: 'ACTIVO',
    color: 'var(--up)',
    bg: 'var(--up-wash)',
    dot: 'var(--up)',
    animation: 'none',
  },
  watching: {
    label: 'VIGILANDO',
    color: 'var(--warn)',
    bg: 'rgba(255,197,61,0.12)',
    dot: 'var(--warn)',
    animation: 'none',
  },
  paused: {
    label: 'PAUSADO',
    color: 'var(--down)',
    bg: 'var(--down-wash)',
    dot: 'var(--down)',
    animation: 'bx-blink calc(1.1s / var(--mo)) steps(1) infinite',
  },
} as const

const styles = {
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--sp-6)',
    padding: '0 var(--sp-5)',
    height: '78px',
    background: 'var(--bg-surface)',
    borderBottom: '1px solid var(--line)',
    flexShrink: 0,
  } as React.CSSProperties,

  brand: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--sp-2)',
    marginRight: 'auto',
  } as React.CSSProperties,

  brandName: {
    fontFamily: 'var(--font-display)',
    fontWeight: 700,
    fontSize: '16px',
    color: 'var(--fg-1)',
    letterSpacing: '-0.02em',
  } as React.CSSProperties,

  brandSub: {
    fontFamily: 'var(--font-sans)',
    fontSize: '11px',
    color: 'var(--fg-3)',
    letterSpacing: '0.04em',
  } as React.CSSProperties,

  cbSection: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '2px',
  },

  cbKey: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
  },

  cbPill: (info: typeof CB_MAP[keyof typeof CB_MAP]) =>
    ({
      display: 'inline-flex',
      alignItems: 'center',
      gap: '5px',
      padding: '4px 10px',
      borderRadius: 'var(--r-pill)',
      background: info.bg,
      color: info.color,
      fontFamily: 'var(--font-mono)',
      fontSize: '11px',
      fontWeight: 600,
      letterSpacing: '0.06em',
      animation: info.animation,
    } as React.CSSProperties),

  cbDot: (info: typeof CB_MAP[keyof typeof CB_MAP]) =>
    ({
      width: '6px',
      height: '6px',
      borderRadius: '50%',
      background: info.dot,
      animation:
        info.label === 'ACTIVO'
          ? 'bx-pulse calc(1.5s / var(--mo)) ease-in-out infinite'
          : 'none',
      flexShrink: 0,
    } as React.CSSProperties),

  pnlSection: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '2px',
    minWidth: '160px',
  },

  pnlKey: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
  },

  pnlValue: (cls: 'is-up' | 'is-down' | 'is-flat') =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '38px',
      lineHeight: 1,
      fontWeight: 600,
      color:
        cls === 'is-up'
          ? 'var(--up)'
          : cls === 'is-down'
          ? 'var(--down)'
          : 'var(--fg-3)',
    } as React.CSSProperties),

  pnlSub: {
    display: 'flex',
    alignItems: 'center',
    gap: '4px',
    fontFamily: 'var(--font-sans)',
    fontSize: '12px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  statGrp: {
    display: 'flex',
    gap: 'var(--sp-5)',
  } as React.CSSProperties,

  stat: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '2px',
  },

  statKey: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
  },

  statValue: {
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
    fontSize: '18px',
    fontWeight: 600,
    color: 'var(--fg-1)',
  } as React.CSSProperties,

  ws: (connected: boolean) =>
    ({
      display: 'flex',
      alignItems: 'center',
      gap: '6px',
      fontFamily: 'var(--font-sans)',
      fontSize: '12px',
      color: connected ? 'var(--up)' : 'var(--down)',
    } as React.CSSProperties),

  wsDot: (connected: boolean) =>
    ({
      width: '6px',
      height: '6px',
      borderRadius: '50%',
      background: connected ? 'var(--up)' : 'var(--down)',
      animation: connected
        ? 'none'
        : 'bx-blink calc(1.1s / var(--mo)) steps(1) infinite',
      flexShrink: 0,
    } as React.CSSProperties),
}

export default function StatusBar() {
  const cb = useMarketStore((s) => s.circuitBreakerState)
  const pnlVal = useMarketStore((s) => s.pnl.total_pnl)
  const winRate = useMarketStore((s) => s.pnl.win_rate)
  const tradeCount = useMarketStore((s) => s.trades.length)
  const wsConnected = useMarketStore((s) => s.wsConnected)
  const prices = useMarketStore((s) => s.prices)
  const latency = useMarketStore((s) => s.latency)
  const execCount = useMarketStore((s) =>
    s.opportunities.filter((o) => o.Status === 'executed').length
  )
  const activeCount = EXCHANGES.filter((ex) => prices[ex] !== null).length

  const cbInfo = CB_MAP[cb] ?? CB_MAP.active
  const pnlCls: 'is-up' | 'is-down' | 'is-flat' =
    pnlVal > 0 ? 'is-up' : pnlVal < 0 ? 'is-down' : 'is-flat'

  return (
    <header style={styles.header} id="bx-top">
      {/* Brand */}
      <div style={styles.brand}>
        <div>
          <div style={styles.brandName}>brecha</div>
          <div style={styles.brandSub}>Arbitrage Engine</div>
        </div>
      </div>

      {/* Circuit Breaker */}
      <div style={styles.cbSection}>
        <span style={styles.cbKey}>Circuit Breaker</span>
        <span style={styles.cbPill(cbInfo)} data-cb={cb}>
          <span style={styles.cbDot(cbInfo)} />
          {cbInfo.label}
        </span>
      </div>

      {/* P&L Hero */}
      <div style={styles.pnlSection}>
        <span style={styles.pnlKey}>P&amp;L acumulado · sesión</span>
        <span
          style={styles.pnlValue(pnlCls)}
          data-testid="pnl-value"
        >
          {fmtSigned(pnlVal)}
        </span>
        <span style={styles.pnlSub}>
          <TrendingUp size={13} strokeWidth={1.75} />
          {tradeCount} trades · {execCount} ejecutadas en vivo
        </span>
      </div>

      {/* Stats */}
      <div style={styles.statGrp}>
        <div style={styles.stat}>
          <span style={styles.statKey}>Tasa de acierto</span>
          <span style={styles.statValue}>{(winRate * 100).toFixed(1)}%</span>
        </div>
        <div style={styles.stat}>
          <span style={styles.statKey}>Exchanges</span>
          <span style={styles.statValue}>
            {activeCount}<small style={{ fontSize: '11px', color: 'var(--fg-3)' }}> / {EXCHANGES.length}</small>
          </span>
        </div>
        <div style={styles.stat}>
          <span style={styles.statKey}>Trades</span>
          <span style={styles.statValue}>{tradeCount}</span>
        </div>
        {latency.samples >= 10 && (
          <div style={styles.stat}>
            <span style={styles.statKey}>Detect µs</span>
            <span style={styles.statValue}>
              p50 {latency.p50.toFixed(1)}
              <small style={{ fontSize: '11px', color: 'var(--fg-3)' }}>
                {' '}/ p99 {latency.p99.toFixed(1)}
              </small>
            </span>
          </div>
        )}
      </div>

      {/* WS indicator */}
      <div style={styles.ws(wsConnected)}>
        <span style={styles.wsDot(wsConnected)} />
        <span>{wsConnected ? 'Connected' : 'Reconnecting…'}</span>
      </div>
    </header>
  )
}
