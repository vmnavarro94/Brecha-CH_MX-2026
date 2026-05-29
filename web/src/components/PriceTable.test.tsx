import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import PriceTable from './PriceTable'

function resetStore() {
  useMarketStore.setState({
    prices: { binance: null, kraken: null, bybit: null },
  })
}

describe('PriceTable', () => {
  beforeEach(() => {
    resetStore()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders 3 rows (binance, kraken, bybit)', () => {
    render(<PriceTable />)
    expect(screen.getByText('Binance')).toBeInTheDocument()
    expect(screen.getByText('Kraken')).toBeInTheDocument()
    expect(screen.getByText('Bybit')).toBeInTheDocument()
  })

  it('shows "Esperando..." when price is null', () => {
    render(<PriceTable />)
    const waiting = screen.getAllByText('Esperando...')
    expect(waiting.length).toBeGreaterThanOrEqual(3)
  })

  it('shows "En vivo" when receivedAt is recent (< 2000ms)', () => {
    const now = Date.now()
    useMarketStore.setState({
      prices: {
        binance: { exchange: 'binance', bid: 60000, ask: 60001, receivedAt: now },
        kraken: null,
        bybit: null,
      },
    })
    render(<PriceTable />)
    expect(screen.getByText('En vivo')).toBeInTheDocument()
  })

  it('shows "Desact." when receivedAt is older than 2000ms', () => {
    const stale = Date.now() - 3000
    useMarketStore.setState({
      prices: {
        binance: { exchange: 'binance', bid: 60000, ask: 60001, receivedAt: stale },
        kraken: null,
        bybit: null,
      },
    })
    render(<PriceTable />)
    expect(screen.getByText('Desact.')).toBeInTheDocument()
  })

  it('renders bid and ask values from store', () => {
    const now = Date.now()
    useMarketStore.setState({
      prices: {
        binance: { exchange: 'binance', bid: 60000.5, ask: 60001.25, receivedAt: now },
        kraken: null,
        bybit: null,
      },
    })
    render(<PriceTable />)
    // Values appear as formatted numbers
    expect(screen.getByText('60,000.50')).toBeInTheDocument()
    expect(screen.getByText('60,001.25')).toBeInTheDocument()
  })
})
