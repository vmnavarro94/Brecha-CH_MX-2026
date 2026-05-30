import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import type { Trade } from '../types/api'
import TradeHistory from './TradeHistory'

function makeTrade(overrides: Partial<Trade> = {}): Trade {
  return {
    ID: crypto.randomUUID(),
    OpportunityID: crypto.randomUUID(),
    BuyExchange: 'binance',
    SellExchange: 'kraken',
    BuyPrice: 60000,
    SellPrice: 60100,
    Volume: 0.001,
    GrossProfit: 10,
    Fees: 0.5,
    NetProfit: 9.5,
    Slippage: 0.1,
    ExecutedAt: new Date().toISOString(),
    RequestedVolume: 0.001,
    PartialFill: false,
    ...overrides,
  }
}

function resetStore() {
  useMarketStore.setState({ trades: [], lastTradeId: null })
}

describe('TradeHistory', () => {
  beforeEach(resetStore)

  it('shows "Sin trades ejecutados aún." when trades is empty', () => {
    render(<TradeHistory />)
    expect(screen.getByText('Sin trades ejecutados aún.')).toBeInTheDocument()
  })

  it('Net P&L positive renders in green with + sign', () => {
    const trade = makeTrade({ NetProfit: 12.5 })
    useMarketStore.setState({ trades: [trade] })
    render(<TradeHistory />)
    // The formatted value appears in both the row cell and the footer total
    const pnlCells = screen.getAllByText('+$12.50')
    expect(pnlCells.length).toBeGreaterThanOrEqual(1)
    // The tbody row cell has the per-row color
    const rowCell = pnlCells.find(
      (el) => el.tagName === 'TD' && el.closest('tbody') !== null
    )
    expect(rowCell).toBeDefined()
    expect(rowCell).toHaveStyle({ color: 'var(--up)' })
  })

  it('Net P&L negative renders in red with unicode minus sign', () => {
    const trade = makeTrade({ NetProfit: -3.2 })
    useMarketStore.setState({ trades: [trade] })
    render(<TradeHistory />)
    const pnlCells = screen.getAllByText('−$3.20')
    expect(pnlCells.length).toBeGreaterThanOrEqual(1)
    const rowCell = pnlCells.find(
      (el) => el.tagName === 'TD' && el.closest('tbody') !== null
    )
    expect(rowCell).toBeDefined()
    expect(rowCell).toHaveStyle({ color: 'var(--down)' })
  })

  it('with 25 trades, first page shows 20 rows', () => {
    const trades = Array.from({ length: 25 }, () => makeTrade())
    useMarketStore.setState({ trades })
    render(<TradeHistory />)
    const rows = document.querySelectorAll('tbody tr')
    expect(rows.length).toBe(20)
  })

  it('"Siguiente" button appears when more than 20 trades', () => {
    const trades = Array.from({ length: 25 }, () => makeTrade())
    useMarketStore.setState({ trades })
    render(<TradeHistory />)
    const next = screen.getByText('Siguiente')
    expect(next).toBeInTheDocument()
    expect(next.closest('button')).not.toBeDisabled()
  })

  it('"Anterior" button is disabled on first page', () => {
    const trades = Array.from({ length: 25 }, () => makeTrade())
    useMarketStore.setState({ trades })
    render(<TradeHistory />)
    const prev = screen.getByText('Anterior')
    expect(prev.closest('button')).toBeDisabled()
  })

  it('footer row sums NetProfit correctly for current page', () => {
    // 5 trades each with NetProfit = 10 → total = 50
    const trades = Array.from({ length: 5 }, () => makeTrade({ NetProfit: 10 }))
    useMarketStore.setState({ trades })
    render(<TradeHistory />)
    // footer shows +$50.00
    expect(screen.getByText('+$50.00')).toBeInTheDocument()
  })

  it('row with PartialFill=true renders a "parcial" badge', () => {
    const trade = makeTrade({ PartialFill: true, RequestedVolume: 0.01, Volume: 0.005 })
    useMarketStore.setState({ trades: [trade] })
    render(<TradeHistory />)
    expect(screen.getByText('parcial')).toBeInTheDocument()
  })

  it('row with PartialFill=false does not render a "parcial" badge', () => {
    const trade = makeTrade({ PartialFill: false })
    useMarketStore.setState({ trades: [trade] })
    render(<TradeHistory />)
    expect(screen.queryByText('parcial')).not.toBeInTheDocument()
  })
})
