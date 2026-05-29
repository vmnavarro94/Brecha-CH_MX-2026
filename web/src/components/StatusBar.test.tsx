import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import StatusBar from './StatusBar'

function resetStore() {
  useMarketStore.setState({
    circuitBreakerState: 'active',
    pnl: { total_pnl: 0, trade_count: 0, win_rate: 0 },
    trades: [],
    opportunities: [],
    wsConnected: true,
  })
}

describe('StatusBar', () => {
  beforeEach(resetStore)

  it('renders "PAUSADO" with red color when circuitBreakerState === "paused"', () => {
    useMarketStore.setState({ circuitBreakerState: 'paused' })
    render(<StatusBar />)
    const pill = screen.getByText('PAUSADO')
    expect(pill).toBeInTheDocument()
    expect(pill.closest('[data-cb]')).toHaveAttribute('data-cb', 'paused')
  })

  it('renders "ACTIVO" with green color when circuitBreakerState === "active"', () => {
    useMarketStore.setState({ circuitBreakerState: 'active' })
    render(<StatusBar />)
    const pill = screen.getByText('ACTIVO')
    expect(pill).toBeInTheDocument()
    expect(pill.closest('[data-cb]')).toHaveAttribute('data-cb', 'active')
  })

  it('renders "VIGILANDO" with yellow color when circuitBreakerState === "watching"', () => {
    useMarketStore.setState({ circuitBreakerState: 'watching' })
    render(<StatusBar />)
    const pill = screen.getByText('VIGILANDO')
    expect(pill).toBeInTheDocument()
    expect(pill.closest('[data-cb]')).toHaveAttribute('data-cb', 'watching')
  })

  it('renders P&L positive with --up color', () => {
    useMarketStore.setState({ pnl: { total_pnl: 124.5, trade_count: 3, win_rate: 0.8 } })
    render(<StatusBar />)
    const pnlEl = screen.getByTestId('pnl-value')
    expect(pnlEl).toHaveStyle({ color: 'var(--up)' })
  })

  it('renders P&L negative with --down color', () => {
    useMarketStore.setState({ pnl: { total_pnl: -3.2, trade_count: 1, win_rate: 0 } })
    render(<StatusBar />)
    const pnlEl = screen.getByTestId('pnl-value')
    expect(pnlEl).toHaveStyle({ color: 'var(--down)' })
  })

  it('renders "Reconnecting..." when wsConnected === false', () => {
    useMarketStore.setState({ wsConnected: false })
    render(<StatusBar />)
    expect(screen.getByText('Reconnecting…')).toBeInTheDocument()
  })

  it('renders "Connected" when wsConnected === true', () => {
    useMarketStore.setState({ wsConnected: true })
    render(<StatusBar />)
    expect(screen.getByText('Connected')).toBeInTheDocument()
  })
})
