import { useState, useEffect } from 'react'
import { ListChecks, ChevronLeft, ChevronRight, ArrowRight } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'
import { fmtSigned, fmtUsd, fmtTime, cap } from '../utils/format'

const PER_PAGE = 20

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

  pager: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
  } as React.CSSProperties,

  pagerInfo: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
    marginRight: '4px',
  } as React.CSSProperties,

  pagerBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    padding: '4px 10px',
    borderRadius: 'var(--r-sm)',
    border: '1px solid var(--line)',
    background: 'transparent',
    color: 'var(--fg-2)',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    cursor: 'pointer',
    transition: 'background var(--dur-fast)',
  } as React.CSSProperties,

  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
    fontSize: '12px',
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

  thR: {
    padding: '8px 12px',
    textAlign: 'right' as const,
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 500,
    letterSpacing: '0.08em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-3)',
    borderBottom: '1px solid var(--line-faint)',
  } as React.CSSProperties,

  td: {
    padding: '8px 12px',
    borderBottom: '1px solid var(--line-faint)',
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  tdR: {
    padding: '8px 12px',
    borderBottom: '1px solid var(--line-faint)',
    textAlign: 'right' as const,
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  tdPnl: (profit: number) =>
    ({
      padding: '8px 12px',
      borderBottom: '1px solid var(--line-faint)',
      textAlign: 'right' as const,
      fontWeight: 600,
      color:
        profit > 0
          ? 'var(--up)'
          : profit < 0
          ? 'var(--down)'
          : 'var(--fg-3)',
    } as React.CSSProperties),

  route: {
    display: 'flex',
    alignItems: 'center',
    gap: '4px',
    color: 'var(--fg-1)',
    fontWeight: 600,
    fontSize: '12px',
  } as React.CSSProperties,

  empty: {
    padding: '32px',
    textAlign: 'center' as const,
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  tfoot: {
    background: 'var(--bg-inset)',
  } as React.CSSProperties,

  tfootTd: {
    padding: '8px 12px',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-3)',
  } as React.CSSProperties,

  tfootTdTotal: (profit: number) =>
    ({
      padding: '8px 12px',
      textAlign: 'right' as const,
      fontFamily: 'var(--font-mono)',
      fontVariantNumeric: 'tabular-nums',
      fontSize: '13px',
      fontWeight: 700,
      color: profit >= 0 ? 'var(--up)' : 'var(--down)',
    } as React.CSSProperties),
}

export default function TradeHistory() {
  const trades = useMarketStore((s) => s.trades)
  const lastId = useMarketStore((s) => s.lastTradeId)
  const [page, setPage] = useState(0)

  const pages = Math.max(1, Math.ceil(trades.length / PER_PAGE))
  const clamped = Math.min(page, pages - 1)

  useEffect(() => {
    if (page !== clamped) setPage(clamped)
  }, [clamped, page])

  const start = clamped * PER_PAGE
  const slice = trades.slice(start, start + PER_PAGE)
  const pageTotal = slice.reduce((a, t) => a + t.NetProfit, 0)

  return (
    <div style={styles.panel} id="bx-trades">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <ListChecks size={13} strokeWidth={1.75} /> Historial de trades
        </span>
        <div style={styles.pager}>
          <span style={styles.pagerInfo}>
            {trades.length} trades · pág. {clamped + 1}/{pages}
          </span>
          <button
            style={styles.pagerBtn}
            disabled={clamped === 0}
            onClick={() => setPage(clamped - 1)}
          >
            <ChevronLeft size={14} strokeWidth={1.75} />
            Anterior
          </button>
          <button
            style={styles.pagerBtn}
            disabled={clamped >= pages - 1}
            onClick={() => setPage(clamped + 1)}
          >
            Siguiente
            <ChevronRight size={14} strokeWidth={1.75} />
          </button>
        </div>
      </div>

      {trades.length === 0 ? (
        <div style={styles.empty}>Sin trades ejecutados aún.</div>
      ) : (
        <table style={styles.table}>
          <thead>
            <tr>
              <th scope="col" style={styles.th}>Hora</th>
              <th scope="col" style={styles.th}>Estrategia</th>
              <th scope="col" style={styles.th}>Par</th>
              <th scope="col" style={styles.thR}>Volumen</th>
              <th scope="col" style={styles.thR}>Precio compra</th>
              <th scope="col" style={styles.thR}>Precio venta</th>
              <th scope="col" style={styles.thR}>Fees</th>
              <th scope="col" style={styles.thR}>Net P&amp;L</th>
            </tr>
          </thead>
          <tbody>
            {slice.map((t) => {
              const strat = t.strategy ?? 'spatial'
              const volUnit = strat === 'spatial' ? 'BTC' : 'USDT'
              const hasPrices = strat === 'spatial'
              return (
                <tr
                  key={t.ID}
                  style={
                    t.ID === lastId && clamped === 0
                      ? { animation: 'bx-rowflash 1s ease-out' }
                      : undefined
                  }
                >
                  <td style={styles.td}>{fmtTime(t.ExecutedAt)}</td>
                  <td style={styles.td}>
                    <span style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '10px',
                      letterSpacing: '0.05em',
                      textTransform: 'uppercase' as const,
                      color: 'var(--fg-2)',
                    }}>{strat}</span>
                  </td>
                  <td style={styles.td}>
                    <span style={styles.route}>
                      <b>{cap(t.BuyExchange)}</b>
                      <ArrowRight size={12} strokeWidth={1.75} style={{ color: 'var(--fg-3)' }} />
                      <b>{cap(t.SellExchange)}</b>
                    </span>
                  </td>
                  <td style={styles.tdR}>
                    {t.Volume.toFixed(strat === 'spatial' ? 8 : 2)}{' '}
                    <span style={{ color: 'var(--fg-3)', fontSize: '10px' }}>{volUnit}</span>
                    {t.PartialFill && (
                      <span style={{
                        marginLeft: '6px',
                        padding: '1px 5px',
                        borderRadius: '3px',
                        background: 'var(--orange)',
                        color: 'var(--bg)',
                        fontSize: '9px',
                        fontWeight: 700,
                        letterSpacing: '0.05em',
                        textTransform: 'uppercase' as const,
                      }}>parcial</span>
                    )}
                  </td>
                  <td style={styles.tdR}>
                    {hasPrices ? fmtUsd(t.BuyPrice) : <span style={{ color: 'var(--fg-3)' }}>—</span>}
                  </td>
                  <td style={styles.tdR}>
                    {hasPrices ? fmtUsd(t.SellPrice) : <span style={{ color: 'var(--fg-3)' }}>—</span>}
                  </td>
                  <td style={styles.tdR}>{fmtUsd(t.Fees, 4)}</td>
                  <td style={styles.tdPnl(t.NetProfit)}>{fmtSigned(t.NetProfit)}</td>
                </tr>
              )
            })}
          </tbody>
          <tfoot style={styles.tfoot}>
            <tr>
              <td colSpan={7} style={styles.tfootTd}>
                Total de la página ({slice.length} trades)
              </td>
              <td style={styles.tfootTdTotal(pageTotal)}>{fmtSigned(pageTotal)}</td>
            </tr>
          </tfoot>
        </table>
      )}
    </div>
  )
}
