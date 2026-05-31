import { create } from 'zustand'
import type {
  CircuitBreakerState, Exchange, Opportunity, PriceData,
  SpreadStats, Trade, PnLSummary
} from '../types/api'
import type { RawOpportunity, RawTrade, RawPriceUpdate } from '../types/api'

export const EXCHANGES: Exchange[] = ['binance', 'kraken', 'bybit', 'okx', 'gate', 'mexc', 'bitget', 'htx', 'cryptocom', 'kucoin']

// Stable palette keyed by hashed pair name so a pair keeps the same color
// regardless of where it lands in the featured ranking.
const PAIR_PALETTE = [
  'var(--orange)',
  'var(--info)',
  'var(--up)',
  'var(--warn)',
  'var(--down)',
  'var(--fg-2)',
]
const DEFAULT_FEATURED = ['binance-okx', 'binance-bybit', 'okx-bybit', 'binance-kraken']
export const MAX_FEATURED = 6

function hashPair(s: string): number {
  let h = 5381
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) + h + s.charCodeAt(i)) | 0
  }
  return h >>> 0
}

// pairColor returns a deterministic palette color for a given pair name.
// The second arg is kept for backward compatibility with existing callers but
// is no longer used — colors are stable per pair.
export function pairColor(pair: string, _featuredPairs?: string[]): string {
  return PAIR_PALETTE[hashPair(pair) % PAIR_PALETTE.length]
}

/** @deprecated kept for tests; use pairColor(pair) */
export const PAIR_COLOR: Record<string, string> = {
  'binance-okx': pairColor('binance-okx'),
  'binance-bybit': pairColor('binance-bybit'),
  'okx-bybit': pairColor('okx-bybit'),
}

export interface ZPoint { t: number; z: number }
export interface TradeMark { t: number; z: number; profit: number }

export interface StrategyPnLRow {
  strategy: string
  total_pnl: number
  trade_count: number
  win_rate: number
  total_volume: number
}

export interface LatencySummary {
  p50: number
  p99: number
  samples: number
  updatesPerSec: number
}

interface MarketState {
  prices: Record<Exchange, PriceData | null>
  opportunities: Opportunity[]
  trades: Trade[]
  pnlHistory: Array<{ time: number; value: number }>
  spreads: SpreadStats[]
  featuredPairs: string[]
  // selectedPairs: when null, featuredPairs is auto-picked (sticky top by Std).
  // When non-null, the user has overridden the selection and we keep it as-is.
  selectedPairs: string[] | null
  zSeries: Record<string, ZPoint[]>
  tradeMarks: Record<string, TradeMark[]>
  circuitBreakerState: CircuitBreakerState
  pnl: PnLSummary
  latency: LatencySummary
  uptime: Record<string, number>
  wsConnected: boolean
  lastTradeId: string | null
  lastOppId: string | null
  strategyPnL: StrategyPnLRow[]
}

interface MarketActions {
  setPrices: (raw: RawPriceUpdate) => void
  addOpportunity: (raw: RawOpportunity) => void
  addTrade: (raw: RawTrade) => void
  setTrades: (raws: RawTrade[]) => void
  setPnL: (summary: { total_pnl: string; trade_count: number; win_rate: number }) => void
  setSpreads: (stats: SpreadStats[]) => void
  setLatency: (p50: number, p99: number, samples: number, updatesPerSec: number) => void
  setUptime: (data: Record<string, number>) => void
  setCircuitBreakerState: (state: CircuitBreakerState) => void
  setWsConnected: (connected: boolean) => void
  setStrategyPnL: (rows: StrategyPnLRow[]) => void
  fetchStrategyPnL: () => Promise<void>
  setSelectedPairs: (pairs: string[] | null) => void
}

function parseOpportunity(raw: RawOpportunity): Opportunity {
  return {
    ...raw,
    BuyPrice: parseFloat(raw.BuyPrice),
    SellPrice: parseFloat(raw.SellPrice),
    NetProfit: parseFloat(raw.NetProfit),
    NetProfitPct: parseFloat(raw.NetProfitPct),
    ZScore: parseFloat(raw.ZScore),
    Score: parseFloat(raw.Score),
    MaxVolume: parseFloat(raw.MaxVolume),
    _t: Date.now(),
  }
}

function parseTrade(raw: RawTrade): Trade {
  return {
    ...raw,
    BuyPrice: parseFloat(raw.BuyPrice),
    SellPrice: parseFloat(raw.SellPrice),
    Volume: parseFloat(raw.Volume),
    GrossProfit: parseFloat(raw.GrossProfit),
    Fees: parseFloat(raw.Fees),
    NetProfit: parseFloat(raw.NetProfit),
    Slippage: parseFloat(raw.Slippage),
    RequestedVolume: raw.RequestedVolume != null ? parseFloat(raw.RequestedVolume) : 0,
    PartialFill: raw.PartialFill ?? false,
  }
}

// Compute z-score for a pair given current prices and spread stats.
// spread direction: (sellBid - buyAsk) / buyAsk — matches backend model.
function computeZ(
  prices: Record<Exchange, PriceData | null>,
  pair: string,
  stat: SpreadStats
): number {
  const [buyEx, sellEx] = pair.split('-') as [Exchange, Exchange]
  const pa = prices[buyEx]
  const pb = prices[sellEx]
  if (!pa || !pb || stat.Std <= 0) return 0
  const spread = (pb.bid - pa.ask) / pa.ask
  return Math.max(-3.6, Math.min(3.6, (spread - stat.Mean) / stat.Std))
}

// Pick featured pairs with hysteresis: keep current pairs that still qualify and
// only fill empty slots with new top-Std candidates. This stops the legend from
// flickering colors/slots every time the Welford std nudges one pair past another.
function pickFeaturedPairs(
  stats: SpreadStats[],
  prev: string[],
  n = 4,
  minSamples = 30,
): string[] {
  const eligible = stats.filter((s) => s.Samples >= minSamples && s.Std > 0)
  if (eligible.length === 0) {
    return prev.length === n ? prev : DEFAULT_FEATURED.slice(0, n)
  }
  const eligibleNames = new Set(eligible.map((s) => s.Pair))
  const sticky = prev.filter((p) => eligibleNames.has(p))
  if (sticky.length >= n) return sticky.slice(0, n)
  const slotsLeft = n - sticky.length
  const fill = eligible
    .filter((s) => !sticky.includes(s.Pair))
    .sort((a, b) => b.Std - a.Std)
    .slice(0, slotsLeft)
    .map((s) => s.Pair)
  const result = [...sticky, ...fill]
  return result.length === n ? result : DEFAULT_FEATURED.slice(0, n)
}

const WINDOW_MS = 60_000

export const useMarketStore = create<MarketState & MarketActions>((set, _get) => ({
  prices: { binance: null, kraken: null, bybit: null, okx: null, gate: null, mexc: null, bitget: null, htx: null, cryptocom: null, kucoin: null },
  opportunities: [],
  trades: [],
  pnlHistory: [],
  spreads: [],
  featuredPairs: DEFAULT_FEATURED.slice(0, 4),
  selectedPairs: null,
  zSeries: {},
  tradeMarks: {},
  circuitBreakerState: 'active',
  pnl: { total_pnl: 0, trade_count: 0, win_rate: 0 },
  latency: { p50: 0, p99: 0, samples: 0, updatesPerSec: 0 },
  uptime: {},
  wsConnected: false,
  lastTradeId: null,
  lastOppId: null,
  strategyPnL: [],

  setPrices: (raw) => set((s) => {
    const now = Date.now()
    const cutoff = now - WINDOW_MS
    const updatedPrices = {
      ...s.prices,
      [raw.exchange]: {
        exchange: raw.exchange,
        bid: parseFloat(raw.bid),
        ask: parseFloat(raw.ask),
        receivedAt: now,
      },
    }
    const zSeries = { ...s.zSeries }
    for (const pair of s.featuredPairs) {
      const stat = s.spreads.find((sp) => sp.Pair === pair)
      if (!stat || stat.Std <= 0) continue
      const z = computeZ(updatedPrices, pair, stat)
      const existing = (zSeries[pair] ?? []).filter((p) => p.t > cutoff)
      zSeries[pair] = [...existing, { t: now, z }]
    }
    return { prices: updatedPrices, zSeries }
  }),

  addOpportunity: (raw) => set((s) => {
    const opp = parseOpportunity(raw)
    const opps = [opp, ...s.opportunities].slice(0, 200)
    return { opportunities: opps, lastOppId: opp.ID }
  }),

  addTrade: (raw) => set((s) => {
    const trade = parseTrade(raw)
    const trades = [trade, ...s.trades]
    const tradeMarks = { ...s.tradeMarks }
    for (const pair of s.featuredPairs) {
      const [buyEx, sellEx] = pair.split('-') as [Exchange, Exchange]
      if (trade.BuyExchange === buyEx && trade.SellExchange === sellEx) {
        const stat = s.spreads.find((sp) => sp.Pair === pair)
        const z = stat ? computeZ(s.prices, pair, stat) : 0
        const marks = [
          { t: Date.now(), z, profit: trade.NetProfit },
          ...(tradeMarks[pair] ?? []),
        ].slice(0, 12)
        tradeMarks[pair] = marks
      }
    }
    return { trades, tradeMarks, lastTradeId: trade.ID }
  }),

  setTrades: (raws) => set({ trades: raws.map(parseTrade) }),

  setPnL: (summary) => set((s) => ({
    pnl: {
      total_pnl: parseFloat(summary.total_pnl),
      trade_count: summary.trade_count,
      win_rate: summary.win_rate,
    },
    pnlHistory: [
      ...s.pnlHistory,
      { time: Date.now(), value: parseFloat(summary.total_pnl) },
    ],
  })),

  setSpreads: (stats) => set((s) => {
    const now = Date.now()
    const cutoff = now - WINDOW_MS
    const featured =
      s.selectedPairs !== null
        ? s.selectedPairs
        : pickFeaturedPairs(stats, s.featuredPairs)
    const zSeries = { ...s.zSeries }
    for (const stat of stats) {
      if (!featured.includes(stat.Pair)) continue
      const z = computeZ(s.prices, stat.Pair, stat)
      const existing = (zSeries[stat.Pair] ?? []).filter((p) => p.t > cutoff)
      zSeries[stat.Pair] = [...existing, { t: now, z }]
    }
    return { spreads: stats, featuredPairs: featured, zSeries }
  }),

  setLatency: (p50, p99, samples, updatesPerSec) => set({ latency: { p50, p99, samples, updatesPerSec } }),
  setUptime: (data) => set({ uptime: data }),
  setCircuitBreakerState: (state) => set({ circuitBreakerState: state }),
  setWsConnected: (connected) => set({ wsConnected: connected }),

  setStrategyPnL: (rows) => set({ strategyPnL: rows }),

  setSelectedPairs: (pairs) => set((s) => {
    if (pairs === null) {
      // Revert to auto-picked featured.
      const auto = pickFeaturedPairs(s.spreads, s.featuredPairs)
      return { selectedPairs: null, featuredPairs: auto }
    }
    const clipped = pairs.slice(0, MAX_FEATURED)
    return { selectedPairs: clipped, featuredPairs: clipped }
  }),

  fetchStrategyPnL: async () => {
    try {
      const r = await fetch('/api/pnl-by-strategy')
      if (!r.ok) return
      const body = await r.json()
      if (Array.isArray(body.strategies)) {
        set({ strategyPnL: body.strategies as StrategyPnLRow[] })
      }
    } catch {
      // silent — polled on next tick
    }
  },
}))
