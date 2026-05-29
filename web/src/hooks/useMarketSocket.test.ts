import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useMarketSocket } from './useMarketSocket'
import { useMarketStore } from '../store/marketStore'

// Mock WebSocket
class MockWS {
  static instances: MockWS[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  readyState = 0
  close = vi.fn()
  constructor() { MockWS.instances.push(this) }
  open() { this.readyState = 1; this.onopen?.() }
  receive(data: object) { this.onmessage?.({ data: JSON.stringify(data) }) }
  drop() { this.readyState = 3; this.onclose?.() }
}

beforeEach(() => {
  MockWS.instances = []
  vi.stubGlobal('WebSocket', MockWS)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    json: () => Promise.resolve(null),
    ok: true,
  }))
  useMarketStore.setState({
    prices: { binance: null, kraken: null, bybit: null },
    opportunities: [],
    trades: [],
    pnlHistory: [],
    spreads: [],
    zSeries: {},
    tradeMarks: {},
    circuitBreakerState: 'active',
    pnl: { total_pnl: 0, trade_count: 0, win_rate: 0 },
    wsConnected: false,
    lastTradeId: null,
    lastOppId: null,
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('useMarketSocket', () => {
  it('connects to WebSocket on mount', () => {
    renderHook(() => useMarketSocket())
    expect(MockWS.instances).toHaveLength(1)
  })

  it('sets wsConnected true on open', () => {
    renderHook(() => useMarketSocket())
    MockWS.instances[0].open()
    expect(useMarketStore.getState().wsConnected).toBe(true)
  })

  it('routes price_update to setPrices', () => {
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.receive({ type: 'price_update', data: { exchange: 'binance', bid: '72000.00', ask: '72001.00' } })
    const price = useMarketStore.getState().prices.binance
    expect(price?.bid).toBe(72000)
    expect(price?.ask).toBe(72001)
  })

  it('routes opportunity to addOpportunity', () => {
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.receive({
      type: 'opportunity',
      data: {
        ID: 'opp-1', BuyExchange: 'binance', SellExchange: 'kraken',
        BuyPrice: '72000', SellPrice: '72250', NetProfit: '1.50',
        NetProfitPct: '0.00208', ZScore: '2.34', Score: '0.78',
        MaxVolume: '0.5', DetectedAt: new Date().toISOString(), Status: 'detected',
      },
    })
    expect(useMarketStore.getState().opportunities).toHaveLength(1)
  })

  it('routes trade_executed to addTrade', () => {
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.receive({
      type: 'trade_executed',
      data: {
        ID: 't-1', OpportunityID: 'opp-1', BuyExchange: 'binance', SellExchange: 'kraken',
        BuyPrice: '72000', SellPrice: '72250', Volume: '0.01',
        GrossProfit: '2.50', Fees: '1.00', NetProfit: '1.50', Slippage: '0.14',
        ExecutedAt: new Date().toISOString(),
      },
    })
    expect(useMarketStore.getState().trades).toHaveLength(1)
  })

  it('routes circuit_breaker to setCircuitBreakerState', () => {
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.receive({ type: 'circuit_breaker', data: { state: 'paused' } })
    expect(useMarketStore.getState().circuitBreakerState).toBe('paused')
  })

  it('sets wsConnected false on close', () => {
    vi.useFakeTimers()
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.drop()
    expect(useMarketStore.getState().wsConnected).toBe(false)
  })

  it('reconnects after close with backoff delay', () => {
    vi.useFakeTimers()
    renderHook(() => useMarketSocket())
    const ws = MockWS.instances[0]
    ws.open()
    ws.drop()
    expect(MockWS.instances).toHaveLength(1)
    vi.advanceTimersByTime(1100)
    expect(MockWS.instances).toHaveLength(2)
  })

  it('re-fetches status and trades on reconnect', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ json: () => Promise.resolve(null) })
    vi.stubGlobal('fetch', fetchMock)
    vi.useFakeTimers()
    renderHook(() => useMarketSocket())
    MockWS.instances[0].open()
    // First connection already fetched
    const callsAfterFirst = fetchMock.mock.calls.length
    MockWS.instances[0].drop()
    vi.advanceTimersByTime(1100)
    MockWS.instances[1].open()
    await vi.runAllTimersAsync()
    expect(fetchMock.mock.calls.length).toBeGreaterThan(callsAfterFirst)
  })
})
