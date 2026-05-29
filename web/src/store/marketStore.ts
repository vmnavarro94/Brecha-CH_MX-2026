import { create } from 'zustand'
import type {
  CircuitBreakerState, Exchange, Opportunity, PriceData,
  SpreadStats, Trade, PnLSummary
} from '../types/api'
import type { RawOpportunity, RawTrade, RawPriceUpdate } from '../types/api'

export const EXCHANGES: Exchange[] = ['binance', 'kraken', 'bybit', 'okx', 'gate']
export const FEATURED_PAIRS = ['binance-okx', 'binance-bybit', 'okx-bybit']
export const PAIR_COLOR: Record<string, string> = {
  'binance-okx': 'var(--orange)',
  'binance-bybit': 'var(--info)',
  'okx-bybit': 'var(--up)',
}

export interface ZPoint { t: number; z: number }
export interface TradeMark { t: number; z: number; profit: number }

interface MarketState {
  prices: Record<Exchange, PriceData | null>
  opportunities: Opportunity[]
  trades: Trade[]
  pnlHistory: Array<{ time: number; value: number }>
  spreads: SpreadStats[]
  zSeries: Record<string, ZPoint[]>
  tradeMarks: Record<string, TradeMark[]>
  circuitBreakerState: CircuitBreakerState
  pnl: PnLSummary
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
  setCircuitBreakerState: (state: CircuitBreakerState) => void
  setWsConnected: (connected: boolean) => void
}

// Parse a raw opportunity's string decimals to numbers
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
  }
}

// Compute z-score for a pair given current prices and spread stats
function computeZ(
  prices: Record<Exchange, PriceData | null>,
  pair: string,
  stat: SpreadStats
): number {
  const [buyEx, sellEx] = pair.split('-') as [Exchange, Exchange]
  const pa = prices[buyEx]
  const pb = prices[sellEx]
  if (!pa || !pb || stat.Std <= 0) return 0
  const spread = (pa.ask - pb.bid) / pa.ask
  return Math.max(-3.6, Math.min(3.6, (spread - stat.Mean) / stat.Std))
}

const WINDOW_MS = 60_000

export const useMarketStore = create<MarketState & MarketActions>((set, _get) => ({
  prices: { binance: null, kraken: null, bybit: null, okx: null, gate: null },
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

  setPrices: (raw) => set((s) => ({
    prices: {
      ...s.prices,
      [raw.exchange]: {
        exchange: raw.exchange,
        bid: parseFloat(raw.bid),
        ask: parseFloat(raw.ask),
        receivedAt: Date.now(),
      },
    },
  })),

  addOpportunity: (raw) => set((s) => {
    const opp = parseOpportunity(raw)
    const opps = [opp, ...s.opportunities].slice(0, 200)
    return { opportunities: opps, lastOppId: opp.ID }
  }),

  addTrade: (raw) => set((s) => {
    const trade = parseTrade(raw)
    const trades = [trade, ...s.trades]
    // Update trade marks for featured pairs
    const tradeMarks = { ...s.tradeMarks }
    for (const pair of FEATURED_PAIRS) {
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
    const zSeries = { ...s.zSeries }
    for (const stat of stats) {
      if (!FEATURED_PAIRS.includes(stat.Pair)) continue
      const z = computeZ(s.prices, stat.Pair, stat)
      const existing = (zSeries[stat.Pair] ?? []).filter((p) => p.t > cutoff)
      zSeries[stat.Pair] = [...existing, { t: now, z }]
    }
    return { spreads: stats, zSeries }
  }),

  setCircuitBreakerState: (state) => set({ circuitBreakerState: state }),
  setWsConnected: (connected) => set({ wsConnected: connected }),
}))
