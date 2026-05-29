export type CircuitBreakerState = 'active' | 'watching' | 'paused'
export type OpportunityStatus = 'detected' | 'executed' | 'skipped' | 'expired'
export type Exchange = 'binance' | 'kraken' | 'bybit' | 'okx' | 'gate' | 'mexc' | 'bitget' | 'htx' | 'cryptocom' | 'kucoin'

export interface PriceData {
  exchange: Exchange
  bid: number
  ask: number
  receivedAt: number
}

export interface Opportunity {
  ID: string
  BuyExchange: Exchange
  SellExchange: Exchange
  BuyPrice: number
  SellPrice: number
  NetProfit: number
  NetProfitPct: number
  ZScore: number
  Score: number
  MaxVolume: number
  DetectedAt: string
  Status: OpportunityStatus
  _t: number  // internal timestamp
}

export interface Trade {
  ID: string
  OpportunityID: string
  BuyExchange: Exchange
  SellExchange: Exchange
  BuyPrice: number
  SellPrice: number
  Volume: number
  GrossProfit: number
  Fees: number
  NetProfit: number
  Slippage: number
  ExecutedAt: string
}

export interface SpreadStats {
  Pair: string
  Mean: number
  Std: number
  Samples: number
}

export interface PnLSummary {
  total_pnl: number
  trade_count: number
  win_rate: number
}

export interface SystemStatus {
  circuit_breaker_state: CircuitBreakerState
  exchange_count: number
  trade_count: number
}

// Raw shapes from backend (string decimals)
export interface RawPriceUpdate {
  exchange: Exchange
  bid: string
  ask: string
}

export interface RawOpportunity {
  ID: string
  BuyExchange: Exchange
  SellExchange: Exchange
  BuyPrice: string
  SellPrice: string
  NetProfit: string
  NetProfitPct: string
  ZScore: string
  Score: string
  MaxVolume: string
  DetectedAt: string
  Status: OpportunityStatus
}

export interface RawTrade {
  ID: string
  OpportunityID: string
  BuyExchange: Exchange
  SellExchange: Exchange
  BuyPrice: string
  SellPrice: string
  Volume: string
  GrossProfit: string
  Fees: string
  NetProfit: string
  Slippage: string
  ExecutedAt: string
}

export type ServerEvent =
  | { type: 'price_update'; data: RawPriceUpdate }
  | { type: 'opportunity'; data: RawOpportunity }
  | { type: 'trade_executed'; data: RawTrade }
  | { type: 'pnl_update'; data: { total_pnl: string; trade_count: number; win_rate: number } }
  | { type: 'circuit_breaker'; data: { state: CircuitBreakerState } }
  | { type: 'spread_stats'; data: SpreadStats[] }
