import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import PriceTable from './PriceTable'

function resetStore() {
  useMarketStore.setState({
    prices: { binance: null, kraken: null, bybit: null, okx: null, gate: null, mexc: null, bitget: null, htx: null, cryptocom: null, kucoin: null },
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

  it('renders 10 rows (all exchanges)', () => {
    render(<PriceTable />)
    expect(screen.getByText('Binance')).toBeInTheDocument()
    expect(screen.getByText('Kraken')).toBeInTheDocument()
    expect(screen.getByText('Bybit')).toBeInTheDocument()
    expect(screen.getByText('OKX')).toBeInTheDocument()
    expect(screen.getByText('Gate.io')).toBeInTheDocument()
    expect(screen.getByText('MEXC')).toBeInTheDocument()
    expect(screen.getByText('Bitget')).toBeInTheDocument()
    expect(screen.getByText('HTX')).toBeInTheDocument()
    expect(screen.getByText('Crypto.com')).toBeInTheDocument()
    expect(screen.getByText('KuCoin')).toBeInTheDocument()
  })

  it('shows "Esperando..." when price is null', () => {
    render(<PriceTable />)
    const waiting = screen.getAllByText('Esperando...')
    expect(waiting.length).toBeGreaterThanOrEqual(10)
  })

  it('shows "En vivo" when receivedAt is recent (< 10000ms)', () => {
    const now = Date.now()
    useMarketStore.setState({
      prices: {
        binance: { exchange: 'binance', bid: 60000, ask: 60001, receivedAt: now },
        kraken: null,
        bybit: null,
        okx: null,
        gate: null,
        mexc: null,
        bitget: null,
        htx: null,
        cryptocom: null,
        kucoin: null,
      },
    })
    render(<PriceTable />)
    expect(screen.getByText('En vivo')).toBeInTheDocument()
  })

  it('shows "Desact." when receivedAt is older than 10000ms', () => {
    const stale = Date.now() - 12_000
    useMarketStore.setState({
      prices: {
        binance: { exchange: 'binance', bid: 60000, ask: 60001, receivedAt: stale },
        kraken: null,
        bybit: null,
        okx: null,
        gate: null,
        mexc: null,
        bitget: null,
        htx: null,
        cryptocom: null,
        kucoin: null,
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
        okx: null,
        gate: null,
        mexc: null,
        bitget: null,
        htx: null,
        cryptocom: null,
        kucoin: null,
      },
    })
    render(<PriceTable />)
    // Values appear as formatted numbers
    expect(screen.getByText('60,000.50')).toBeInTheDocument()
    expect(screen.getByText('60,001.25')).toBeInTheDocument()
  })
})
