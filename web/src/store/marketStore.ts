import { create } from 'zustand'
import type {
  CircuitBreakerState, Exchange, Opportunity, PriceData,
  SpreadStats, Trade, PnLSummary
} from '../types/api'
import type { RawOpportunity, RawTrade, RawPriceUpdate } from '../types/api'

export const EXCHANGES: Exchange[] = ['binance', 'kraken', 'bybit', 'okx', 'gate', 'mexc', 'bitget', 'htx', 'cryptocom', 'kucoin']

const PAIR_COLORS = ['var(--orange)', 'var(--info)', 'var(--up)', 'var(--fg-2)']
const DEFAULT_FEATURED = ['binance-okx', 'binance-bybit', 'okx-bybit']

export function pairColor(pair: string, featuredPairs: string[]): string {
  const i = featuredPairs.indexOf(pair)
  return PAIR_COLORS[i >= 0 ? i : 0]
}

/** @deprecated use pairColor(pair, featuredPairs) */
export const PAIR_COLOR: Record<string, string> = {
  'binance-okx': 'var(--orange)',
  'binance-bybit': 'var(--info)',
  'okx-bybit': 'var(--up)',
}

export interface ZPoint { t: number; z: number }
export interface TradeMark { t: number; z: number; profit: number }

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
  zSeries: Record<string, ZPoint[]>
  tradeMarks: Record<string, TradeMark[]>
  circuitBreakerState: CircuitBreakerState
  pnl: PnLSummary
  latency: LatencySummary
  uptime: Record<string, number>
  wsConnected: boolean
  lastTradeId: string | null
  lastOppId: string | null
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

// Pick top N pairs by std, requiring minSamples. Falls back to defaults if not enough.
function pickFeaturedPairs(stats: SpreadStats[], n = 3, minSamples = 50): string[] {
  const candidates = stats
    .filter((s) => s.Samples >= minSamples && s.Std > 0)
    .sort((a, b) => b.Std - a.Std)
    .slice(0, n)
    .map((s) => s.Pair)
  return candidates.length === n ? candidates : DEFAULT_FEATURED
}

const WINDOW_MS = 60_000

export const useMarketStore = create<MarketState & MarketActions>((set, _get) => ({
  prices: { binance: null, kraken: null, bybit: null, okx: null, gate: null, mexc: null, bitget: null, htx: null, cryptocom: null, kucoin: null },
  opportunities: [],
  trades: [],
  pnlHistory: [],
  spreads: [],
  featuredPairs: DEFAULT_FEATURED,
  zSeries: {},
  tradeMarks: {},
  circuitBreakerState: 'active',
  pnl: { total_pnl: 0, trade_count: 0, win_rate: 0 },
  latency: { p50: 0, p99: 0, samples: 0, updatesPerSec: 0 },
  uptime: {},
  wsConnected: false,
  lastTradeId: null,
  lastOppId: null,

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
    const featured = pickFeaturedPairs(stats)
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
}))
