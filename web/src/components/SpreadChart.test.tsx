import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import SpreadChart from './SpreadChart'

// ResizeObserver is not available in jsdom
global.ResizeObserver = vi.fn().mockImplementation(() => ({
  observe: vi.fn(),
  unobserve: vi.fn(),
  disconnect: vi.fn(),
}))

function resetStore() {
  useMarketStore.setState({
    zSeries: {},
    spreads: [],
    tradeMarks: {},
  })
}

describe('SpreadChart', () => {
  beforeEach(resetStore)

  it('renders warming banner when Samples < 100', () => {
    useMarketStore.setState({
      spreads: [{ Pair: 'binance-kraken', Mean: 0, Std: 0.001, Samples: 42 }],
      zSeries: { 'binance-kraken': [{ t: Date.now(), z: 0.5 }] },
    })
    render(<SpreadChart />)
    expect(screen.getByText(/Calentando modelo/)).toBeInTheDocument()
    expect(screen.getByText(/42\/100 samples/)).toBeInTheDocument()
  })

  it('does NOT render warming banner when Samples >= 100', () => {
    useMarketStore.setState({
      spreads: [{ Pair: 'binance-kraken', Mean: 0, Std: 0.001, Samples: 100 }],
      zSeries: { 'binance-kraken': [{ t: Date.now(), z: 0.5 }] },
    })
    render(<SpreadChart />)
    expect(screen.queryByText(/Calentando modelo/)).not.toBeInTheDocument()
  })

  it('renders reference line labels for y=1, y=2, y=-1, y=-2', () => {
    render(<SpreadChart />)
    expect(screen.getByText('+2σ')).toBeInTheDocument()
    expect(screen.getByText('+1σ')).toBeInTheDocument()
    expect(screen.getByText('−1σ')).toBeInTheDocument()
    expect(screen.getByText('−2σ')).toBeInTheDocument()
  })

  it('renders trade markers when tradeMarks has entries', () => {
    const now = Date.now()
    useMarketStore.setState({
      tradeMarks: {
        'binance-kraken': [{ t: now, z: 1.5, profit: 10 }],
      },
    })
    render(<SpreadChart />)
    // Trade markers are circles in an SVG — check the aria-label or data attribute
    const markers = document.querySelectorAll('[data-testid="trade-marker"]')
    expect(markers.length).toBeGreaterThanOrEqual(1)
  })
})
