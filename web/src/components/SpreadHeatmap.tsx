import { useEffect, useRef, useState } from 'react'
import { useMarketStore, EXCHANGES } from '../store/marketStore'
import { Grid3x3 } from 'lucide-react'

const FLASH_MS = 1500

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

  meta: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  body: {
    padding: '12px 14px 14px',
  } as React.CSSProperties,

  table: {
    borderCollapse: 'collapse' as const,
    fontFamily: 'var(--font-mono)',
    fontSize: '9px',
    width: '100%',
  } as React.CSSProperties,

  cornerCell: {
    width: '50px',
    color: 'var(--fg-3)',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    fontSize: '8px',
    fontWeight: 500,
    textAlign: 'left' as const,
    padding: '0 4px',
  } as React.CSSProperties,

  colLabel: {
    color: 'var(--fg-3)',
    textTransform: 'capitalize' as const,
    padding: '4px 0',
    textAlign: 'center' as const,
    fontWeight: 500,
    fontSize: '9px',
  } as React.CSSProperties,

  rowLabel: {
    color: 'var(--fg-3)',
    textTransform: 'capitalize' as const,
    padding: '0 6px',
    fontWeight: 500,
    fontSize: '9px',
    textAlign: 'right' as const,
  } as React.CSSProperties,

  cell: (color: string) =>
    ({
      width: '24px',
      height: '24px',
      background: color,
      border: '1px solid var(--bg-base)',
      textAlign: 'center' as const,
      verticalAlign: 'middle' as const,
      color: 'var(--bg-base)',
      fontWeight: 600,
      fontSize: '8px',
    } as React.CSSProperties),

  diagonal: {
    background: 'var(--bg-inset)',
    width: '24px',
    height: '24px',
    border: '1px solid var(--bg-base)',
  } as React.CSSProperties,

  empty: {
    background: 'var(--bg-surface-2)',
    width: '24px',
    height: '24px',
    border: '1px solid var(--bg-base)',
  } as React.CSSProperties,

  legend: {
    display: 'flex',
    alignItems: 'center',
    gap: '12px',
    paddingTop: '10px',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  legendBar: {
    display: 'flex',
    height: '6px',
    width: '120px',
    borderRadius: 'var(--r-pill)',
    overflow: 'hidden',
    background: 'linear-gradient(90deg, rgba(255,90,90,0.7) 0%, var(--bg-inset) 50%, rgba(31,203,138,0.7) 100%)',
  } as React.CSSProperties,
}

const SHORT: Record<string, string> = {
  binance: 'Bnce',
  kraken: 'Krkn',
  bybit: 'Bybt',
  okx: 'OKX',
  gate: 'Gate',
  mexc: 'MEXC',
  bitget: 'Btgt',
  htx: 'HTX',
  cryptocom: 'Cro',
  kucoin: 'KCN',
}

function colorFor(spreadPct: number): string {
  if (spreadPct === 0) return 'var(--bg-inset)'
  const mag = Math.min(1, Math.abs(spreadPct) / 0.05)
  if (spreadPct > 0) {
    const a = 0.18 + mag * 0.7
    return `rgba(31, 203, 138, ${a.toFixed(3)})`
  }
  const a = 0.15 + mag * 0.55
  return `rgba(255, 90, 90, ${a.toFixed(3)})`
}

export default function SpreadHeatmap() {
  const prices = useMarketStore((s) => s.prices)
  const trades = useMarketStore((s) => s.trades)
  const lastTradeId = useMarketStore((s) => s.lastTradeId)
  const flashes = useRef<Map<string, { t: number; profit: number }>>(new Map())
  const [, force] = useState(0)

  // Register a flash when a new trade arrives.
  useEffect(() => {
    if (!lastTradeId || trades.length === 0) return
    const t = trades[0]
    if (t.ID !== lastTradeId) return
    flashes.current.set(`${t.BuyExchange}|${t.SellExchange}`, {
      t: Date.now(),
      profit: t.NetProfit,
    })
    force((n) => n + 1)
  }, [lastTradeId, trades])

  // Tick to drop expired flashes.
  useEffect(() => {
    const iv = setInterval(() => {
      const now = Date.now()
      let changed = false
      for (const [k, v] of flashes.current.entries()) {
        if (now - v.t > FLASH_MS) {
          flashes.current.delete(k)
          changed = true
        }
      }
      if (changed) force((n) => n + 1)
    }, 250)
    return () => clearInterval(iv)
  }, [])

  function spreadPct(buyEx: string, sellEx: string): number | null {
    const a = prices[buyEx as keyof typeof prices]
    const b = prices[sellEx as keyof typeof prices]
    if (!a || !b || a.ask <= 0) return null
    return ((b.bid - a.ask) / a.ask) * 100
  }

  function flashStyle(buy: string, sell: string): React.CSSProperties | undefined {
    const f = flashes.current.get(`${buy}|${sell}`)
    if (!f) return undefined
    const glow = f.profit >= 0 ? 'rgba(31,203,138,0.9)' : 'rgba(255,90,90,0.9)'
    return {
      outline: `2px solid ${glow}`,
      outlineOffset: '-1px',
      boxShadow: `0 0 8px 1px ${glow}`,
      transition: 'box-shadow 200ms ease-out, outline 200ms ease-out',
    }
  }

  let countPositive = 0
  for (const buy of EXCHANGES) {
    for (const sell of EXCHANGES) {
      if (buy === sell) continue
      const s = spreadPct(buy, sell)
      if (s !== null && s > 0) countPositive++
    }
  }

  return (
    <div style={styles.panel} id="bx-heatmap">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <Grid3x3 size={13} strokeWidth={1.75} /> Spreads · matriz buy → sell
        </span>
        <span style={styles.meta}>
          {countPositive} pares con spread &gt; 0
        </span>
      </div>

      <div style={styles.body}>
        <table style={styles.table}>
          <thead>
            <tr>
              <th style={styles.cornerCell}>BUY ↓</th>
              {EXCHANGES.map((ex) => (
                <th key={ex} style={styles.colLabel}>{SHORT[ex] ?? ex}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {EXCHANGES.map((buy) => (
              <tr key={buy}>
                <td style={styles.rowLabel}>{SHORT[buy] ?? buy}</td>
                {EXCHANGES.map((sell) => {
                  if (buy === sell) return <td key={sell} style={styles.diagonal} />
                  const s = spreadPct(buy, sell)
                  if (s === null) return <td key={sell} style={styles.empty} />
                  const flash = flashStyle(buy, sell)
                  return (
                    <td
                      key={sell}
                      style={{ ...styles.cell(colorFor(s)), ...(flash ?? {}) }}
                      title={`buy ${buy} → sell ${sell}: ${s.toFixed(4)}%`}
                    >
                      {Math.abs(s) >= 0.005 ? s.toFixed(2) : ''}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>

        <div style={styles.legend}>
          <span>−5%</span>
          <span style={styles.legendBar} />
          <span>+5%</span>
          <span style={{ marginLeft: 'auto' }}>spread % = (sellBid − buyAsk) / buyAsk</span>
        </div>
      </div>
    </div>
  )
}
