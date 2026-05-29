package types

import (
	"time"

	"github.com/shopspring/decimal"
)

type PriceUpdate struct {
	Exchange   string
	Bid        decimal.Decimal
	Ask        decimal.Decimal
	BidSize    decimal.Decimal
	AskSize    decimal.Decimal
	ReceivedAt time.Time
	ExchangeAt time.Time
}

func (p *PriceUpdate) IsStale(threshold time.Duration) bool {
	return time.Since(p.ReceivedAt) > threshold
}

type OpportunityStatus string

const (
	StatusDetected OpportunityStatus = "detected"
	StatusExecuted OpportunityStatus = "executed"
	StatusSkipped  OpportunityStatus = "skipped"
	StatusExpired  OpportunityStatus = "expired"
)

type Opportunity struct {
	ID           string
	BuyExchange  string
	SellExchange string
	BuyPrice     decimal.Decimal
	SellPrice    decimal.Decimal
	NetProfit    decimal.Decimal
	NetProfitPct decimal.Decimal
	ZScore       decimal.Decimal
	Score        decimal.Decimal
	MaxVolume    decimal.Decimal
	DetectedAt   time.Time
	Status       OpportunityStatus
}

type Trade struct {
	ID            string
	OpportunityID string
	BuyExchange   string
	SellExchange  string
	BuyPrice      decimal.Decimal
	SellPrice     decimal.Decimal
	Volume        decimal.Decimal
	GrossProfit   decimal.Decimal
	Fees          decimal.Decimal
	NetProfit     decimal.Decimal
	Slippage      decimal.Decimal
	ExecutedAt    time.Time
}
