import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import PnLChart from './PnLChart'

global.ResizeObserver = vi.fn().mockImplementation(() => ({
  observe: vi.fn(),
  unobserve: vi.fn(),
  disconnect: vi.fn(),
}))

function resetStore() {
  useMarketStore.setState({ pnlHistory: [] })
}

describe('PnLChart', () => {
  beforeEach(resetStore)

  it('shows "Esperando primer trade..." when pnlHistory is empty', () => {
    render(<PnLChart />)
    expect(screen.getByText(/Esperando primer trade/)).toBeInTheDocument()
  })

  it('shows "Esperando primer trade..." when pnlHistory has 1 entry', () => {
    useMarketStore.setState({
      pnlHistory: [{ time: Date.now(), value: 10 }],
    })
    render(<PnLChart />)
    expect(screen.getByText(/Esperando primer trade/)).toBeInTheDocument()
  })

  it('renders chart when pnlHistory has 2+ entries', () => {
    const now = Date.now()
    useMarketStore.setState({
      pnlHistory: [
        { time: now - 10000, value: 0 },
        { time: now, value: 50 },
      ],
    })
    render(<PnLChart />)
    expect(screen.queryByText(/Esperando primer trade/)).not.toBeInTheDocument()
    // SVG chart should be rendered
    const svg = document.querySelector('svg')
    expect(svg).toBeInTheDocument()
  })
})
