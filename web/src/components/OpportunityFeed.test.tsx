import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { useMarketStore } from '../store/marketStore'
import type { Opportunity } from '../types/api'
import OpportunityFeed from './OpportunityFeed'

function makeOpp(overrides: Partial<Opportunity> = {}): Opportunity {
  return {
    ID: crypto.randomUUID(),
    BuyExchange: 'binance',
    SellExchange: 'kraken',
    BuyPrice: 60000,
    SellPrice: 60100,
    NetProfit: 10,
    NetProfitPct: 0.00208,
    ZScore: 1.5,
    Score: 0.75,
    MaxVolume: 0.01,
    DetectedAt: new Date().toISOString(),
    Status: 'detected',
    _t: Date.now(),
    ...overrides,
  }
}

function resetStore() {
  useMarketStore.setState({ opportunities: [], lastOppId: null })
}

describe('OpportunityFeed', () => {
  beforeEach(resetStore)

  it('shows empty state when opportunities array is empty', () => {
    render(<OpportunityFeed />)
    expect(screen.getByText(/El motor está escuchando/)).toBeInTheDocument()
  })

  it('renders "Ejecutada" badge in green for executed opportunity', () => {
    const opp = makeOpp({ Status: 'executed' })
    useMarketStore.setState({ opportunities: [opp] })
    render(<OpportunityFeed />)
    const badge = screen.getByText('Ejecutada')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('[data-status]')).toHaveAttribute('data-status', 'executed')
  })

  it('renders "Descartada" badge for skipped opportunity', () => {
    const opp = makeOpp({ Status: 'skipped' })
    useMarketStore.setState({ opportunities: [opp] })
    render(<OpportunityFeed />)
    expect(screen.getByText('Descartada')).toBeInTheDocument()
  })

  it('renders "Expirada" badge for expired opportunity', () => {
    const opp = makeOpp({ Status: 'expired' })
    useMarketStore.setState({ opportunities: [opp] })
    render(<OpportunityFeed />)
    expect(screen.getByText('Expirada')).toBeInTheDocument()
  })

  it('renders z-score badge as "hot" when |ZScore| > 2', () => {
    const opp = makeOpp({ ZScore: 2.5 })
    useMarketStore.setState({ opportunities: [opp] })
    render(<OpportunityFeed />)
    const badge = document.querySelector('[data-hot="true"]')
    expect(badge).toBeInTheDocument()
  })

  it('renders z-score badge as NOT hot when |ZScore| <= 2', () => {
    const opp = makeOpp({ ZScore: 1.5 })
    useMarketStore.setState({ opportunities: [opp] })
    render(<OpportunityFeed />)
    const badge = document.querySelector('[data-hot="false"]')
    expect(badge).toBeInTheDocument()
  })

  it('limits visible rows to 50', () => {
    const opps = Array.from({ length: 80 }, (_, i) => makeOpp({ ID: String(i) }))
    useMarketStore.setState({ opportunities: opps })
    render(<OpportunityFeed />)
    const rows = document.querySelectorAll('[data-opp-row]')
    expect(rows.length).toBe(50)
  })
})
