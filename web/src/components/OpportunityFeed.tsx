import { ArrowLeftRight, Check, Minus, Clock, Search, ArrowRight } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'
import type { Opportunity, OpportunityStatus } from '../types/api'
import { fmtPct, fmtTime, cap } from '../utils/format'

interface StatusInfo {
  color: string
  bg: string
  icon: React.ReactNode
  label: string
}

const ST_MAP: Record<OpportunityStatus, StatusInfo> = {
  executed: {
    color: 'var(--up)',
    bg: 'var(--up-wash)',
    icon: <Check size={11} strokeWidth={1.75} />,
    label: 'Ejecutada',
  },
  skipped: {
    color: 'var(--fg-3)',
    bg: 'rgba(113,123,137,0.12)',
    icon: <Minus size={11} strokeWidth={1.75} />,
    label: 'Descartada',
  },
  expired: {
    color: 'var(--orange)',
    bg: 'var(--orange-wash)',
    icon: <Clock size={11} strokeWidth={1.75} />,
    label: 'Expirada',
  },
  detected: {
    color: 'var(--info)',
    bg: 'var(--info-wash)',
    icon: <Search size={11} strokeWidth={1.75} />,
    label: 'Detectada',
  },
}

const styles = {
  panel: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-lg)',
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column' as const,
    maxHeight: '480px',
  } as React.CSSProperties,

  head: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 15px',
    borderBottom: '1px solid var(--line-faint)',
    flexShrink: 0,
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

  rows: {
    overflowY: 'auto' as const,
    flex: 1,
  } as React.CSSProperties,

  emptyState: {
    padding: '28px 16px',
    textAlign: 'center' as const,
    color: 'var(--fg-3)',
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
  } as React.CSSProperties,

  row: (fresh: boolean) =>
    ({
      padding: '10px 14px',
      borderBottom: '1px solid var(--line-faint)',
      animation: fresh
        ? 'bx-slidein calc(0.34s / var(--mo)) var(--ease-out)'
        : 'none',
    } as React.CSSProperties),

  rowTop: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    marginBottom: '5px',
  } as React.CSSProperties,

  rowBottom: {
    display: 'flex',
    alignItems: 'center',
    gap: '10px',
  } as React.CSSProperties,

  route: {
    display: 'flex',
    alignItems: 'center',
    gap: '4px',
    fontFamily: 'var(--font-sans)',
    fontSize: '13px',
    fontWeight: 600,
    color: 'var(--fg-1)',
    flex: 1,
  } as React.CSSProperties,

  badge: (info: StatusInfo) =>
    ({
      display: 'inline-flex',
      alignItems: 'center',
      gap: '4px',
      padding: '2px 7px',
      borderRadius: 'var(--r-pill)',
      background: info.bg,
      color: info.color,
      fontFamily: 'var(--font-mono)',
      fontSize: '10px',
      fontWeight: 600,
      letterSpacing: '0.04em',
      flexShrink: 0,
    } as React.CSSProperties),

  time: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
    marginLeft: 'auto',
    flexShrink: 0,
  } as React.CSSProperties,

  pct: (up: boolean) =>
    ({
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '13px',
      fontWeight: 600,
      color: up ? 'var(--up)' : 'var(--down)',
      minWidth: '72px',
    } as React.CSSProperties),

  zwrap: {
    display: 'flex',
    alignItems: 'center',
    gap: '2px',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  zbadge: (hot: boolean) =>
    ({
      display: 'inline-block',
      padding: '1px 5px',
      borderRadius: 'var(--r-xs)',
      background: hot ? 'var(--orange-wash)' : 'transparent',
      color: hot ? 'var(--orange)' : 'var(--fg-2)',
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '11px',
      fontWeight: hot ? 600 : 400,
    } as React.CSSProperties),

  scoreWrap: {
    display: 'flex',
    alignItems: 'center',
    gap: '5px',
    marginLeft: 'auto',
  } as React.CSSProperties,

  scoreBar: {
    width: '50px',
    height: '4px',
    background: 'var(--line)',
    borderRadius: 'var(--r-pill)',
    overflow: 'hidden',
  } as React.CSSProperties,

  scoreBarFill: (pct: number) =>
    ({
      height: '100%',
      width: `${pct}%`,
      background: 'linear-gradient(90deg, var(--orange-dim), var(--orange))',
      borderRadius: 'var(--r-pill)',
    } as React.CSSProperties),

  scoreVal: {
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
    fontSize: '10px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,
}

interface OppRowProps {
  op: Opportunity
  fresh: boolean
}

function OppRow({ op, fresh }: OppRowProps) {
  const st = ST_MAP[op.Status] ?? ST_MAP.detected
  const hot = Math.abs(op.ZScore) > 2
  const up = op.NetProfitPct >= 0

  return (
    <div style={styles.row(fresh)} data-opp-row>
      <div style={styles.rowTop}>
        <span style={styles.route}>
          <b>{cap(op.BuyExchange)}</b>
          <ArrowRight size={13} strokeWidth={1.75} style={{ color: 'var(--fg-3)' }} />
          <b>{cap(op.SellExchange)}</b>
        </span>
        <span style={styles.badge(st)} data-status={op.Status}>
          {st.icon}
          {st.label}
        </span>
        <span style={styles.time}>{fmtTime(op.DetectedAt)}</span>
      </div>
      <div style={styles.rowBottom}>
        <span style={styles.pct(up)}>{fmtPct(op.NetProfitPct)}</span>
        <span style={styles.zwrap}>
          z
          <span style={styles.zbadge(hot)} data-hot={String(hot)}>
            {op.ZScore >= 0 ? '+' : '−'}{Math.abs(op.ZScore).toFixed(2)}
          </span>
        </span>
        <span style={styles.scoreWrap}>
          <span style={styles.scoreBar}>
            <span style={styles.scoreBarFill(op.Score * 100)} />
          </span>
          <span style={styles.scoreVal}>{op.Score.toFixed(2)}</span>
        </span>
      </div>
    </div>
  )
}

export default function OpportunityFeed() {
  const opps = useMarketStore((s) => s.opportunities)
  const lastId = useMarketStore((s) => s.lastOppId)
  const visible = opps.slice(0, 50)

  return (
    <div style={styles.panel} id="bx-feed">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <ArrowLeftRight size={13} strokeWidth={1.75} /> Oportunidades en vivo
        </span>
        <span style={styles.meta}>{opps.length} en buffer · top 50</span>
      </div>
      <div style={styles.rows}>
        {visible.length === 0 && (
          <div style={styles.emptyState}>
            El motor está escuchando. Sin oportunidades por ahora.
          </div>
        )}
        {visible.map((op) => (
          <OppRow key={op.ID} op={op} fresh={op.ID === lastId} />
        ))}
      </div>
    </div>
  )
}
