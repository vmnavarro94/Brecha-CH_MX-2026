import { useEffect, useRef, useState, memo } from 'react'
import { Radio } from 'lucide-react'
import { useMarketStore, EXCHANGES } from '../store/marketStore'
import type { Exchange, PriceData } from '../types/api'

const EX_META: Record<Exchange, { label: string; color: string }> = {
  binance: { label: 'Binance', color: '#F3BA2F' },
  kraken: { label: 'Kraken', color: '#7B68EE' },
  bybit: { label: 'Bybit', color: '#F7A600' },
  okx: { label: 'OKX', color: '#4086FF' },
  gate: { label: 'Gate.io', color: '#2ECC71' },
  mexc: { label: 'MEXC', color: '#00C2CB' },
  bitget: { label: 'Bitget', color: '#FF6B35' },
  htx: { label: 'HTX', color: '#1F89E5' },
  cryptocom: { label: 'Crypto.com', color: '#002D74' },
  kucoin: { label: 'KuCoin', color: '#24AE8F' },
}

function px2(n: number): string {
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const tableStyles = {
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

  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
  } as React.CSSProperties,

  th: {
    padding: '8px 12px',
    textAlign: 'left' as const,
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 500,
    letterSpacing: '0.08em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-3)',
    borderBottom: '1px solid var(--line-faint)',
  } as React.CSSProperties,

  td: {
    padding: '10px 12px',
    borderBottom: '1px solid var(--line-faint)',
    fontSize: '13px',
  } as React.CSSProperties,
}

interface PriceRowProps {
  ex: Exchange
  p: PriceData | null
  now: number
}

function PriceRow({ ex, p, now }: PriceRowProps) {
  const prevRef = useRef<{ bid: number | null; ask: number | null }>({ bid: null, ask: null })
  const [flash, setFlash] = useState({ bid: '', ask: '' })
  const meta = EX_META[ex]

  useEffect(() => {
    if (!p) return
    const next = { bid: '', ask: '' }
    if (prevRef.current.bid != null && p.bid !== prevRef.current.bid) {
      next.bid = p.bid > prevRef.current.bid ? 'flash-up' : 'flash-down'
    }
    if (prevRef.current.ask != null && p.ask !== prevRef.current.ask) {
      next.ask = p.ask > prevRef.current.ask ? 'flash-up' : 'flash-down'
    }
    prevRef.current = { bid: p.bid, ask: p.ask }
    if (next.bid || next.ask) {
      setFlash(next)
      const t = setTimeout(() => setFlash({ bid: '', ask: '' }), 300)
      return () => clearTimeout(t)
    }
  }, [p?.bid, p?.ask]) // eslint-disable-line react-hooks/exhaustive-deps

  let statusLabel = 'Esperando...'
  let statusColor = 'var(--fg-3)'
  let dotAnimation = 'none'

  if (p) {
    const age = now - p.receivedAt
    if (age < 10_000) {
      statusLabel = 'En vivo'
      statusColor = 'var(--up)'
      dotAnimation = 'bx-pulse calc(1.5s / var(--mo)) ease-in-out infinite'
    } else {
      statusLabel = 'Desact.'
      statusColor = 'var(--warn)'
    }
  }

  const spread = p ? ((p.ask - p.bid) / p.ask) * 100 : null

  const flashBg = (cls: string) => {
    if (cls === 'flash-up') return 'var(--up-wash)'
    if (cls === 'flash-down') return 'var(--down-wash)'
    return 'transparent'
  }

  return (
    <tr>
      <td style={tableStyles.td}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span
            style={{
              background: meta.color + '22',
              color: meta.color,
              fontFamily: 'var(--font-mono)',
              fontSize: '10px',
              fontWeight: 600,
              padding: '2px 6px',
              borderRadius: 'var(--r-sm)',
            }}
          >
            {meta.label[0]}
          </span>
          <span style={{ color: 'var(--fg-1)', fontSize: '13px' }}>{meta.label}</span>
        </div>
      </td>
      <td style={tableStyles.td}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontVariantNumeric: 'tabular-nums',
            fontSize: '13px',
            color: 'var(--fg-1)',
            background: flashBg(flash.bid),
            padding: '2px 4px',
            borderRadius: 'var(--r-xs)',
            transition: 'background 0.1s',
            display: 'inline-block',
          }}
        >
          {p ? px2(p.bid) : '—'}
        </span>
      </td>
      <td style={tableStyles.td}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontVariantNumeric: 'tabular-nums',
            fontSize: '13px',
            color: 'var(--fg-1)',
            background: flashBg(flash.ask),
            padding: '2px 4px',
            borderRadius: 'var(--r-xs)',
            transition: 'background 0.1s',
            display: 'inline-block',
          }}
        >
          {p ? px2(p.ask) : '—'}
        </span>
      </td>
      <td style={tableStyles.td}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontVariantNumeric: 'tabular-nums',
            fontSize: '12px',
            color: 'var(--fg-2)',
          }}
        >
          {spread != null ? spread.toFixed(4) + '%' : '—'}
        </span>
      </td>
      <td style={tableStyles.td}>
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: '5px',
            color: statusColor,
            fontFamily: 'var(--font-mono)',
            fontSize: '11px',
          }}
        >
          <span
            style={{
              width: '5px',
              height: '5px',
              borderRadius: '50%',
              background: statusColor,
              animation: dotAnimation,
              display: 'inline-block',
            }}
          />
          {statusLabel}
        </span>
      </td>
    </tr>
  )
}

function PriceTable() {
  const prices = useMarketStore((s) => s.prices)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(iv)
  }, [])

  return (
    <div style={tableStyles.panel} id="bx-prices">
      <div style={tableStyles.head}>
        <span style={tableStyles.eyebrow}>
          <Radio size={13} strokeWidth={1.75} /> Precios en vivo · BBO
        </span>
        <span style={tableStyles.meta}>BTC/USDT · 250 ms</span>
      </div>
      <table style={tableStyles.table}>
        <thead>
          <tr>
            <th scope="col" style={tableStyles.th}>Exchange</th>
            <th scope="col" style={tableStyles.th}>Bid</th>
            <th scope="col" style={tableStyles.th}>Ask</th>
            <th scope="col" style={tableStyles.th}>Spread</th>
            <th scope="col" style={tableStyles.th}>Estado</th>
          </tr>
        </thead>
        <tbody>
          {EXCHANGES.map((ex) => (
            <PriceRow key={ex} ex={ex} p={prices[ex]} now={now} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default memo(PriceTable)
