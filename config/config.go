package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port string

	BinanceWSURL string
	KrakenWSURL  string
	BybitWSURL   string
	OKXWSURL     string
	GateWSURL    string
	MEXCWSURL       string
	BitgetWSURL     string
	HTXWSURL        string
	CryptoComWSURL  string
	KuCoinAPIURL    string

	MinNetProfitPct    float64
	MaxPositionUSDT    float64
	ExecutionInterval  time.Duration
	OpportunityTTL     time.Duration
	StalenessThreshold time.Duration

	WindowSize int
	MinSamples int

	CircuitBreakerN         int
	CircuitBreakerLossPct   float64
	CircuitBreakerPauseMins int

	InitialUSDTPerExchange float64
	InitialBTCPerExchange  float64

	AllowedOrigin string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", "8080"),
		BinanceWSURL:       getEnv("BINANCE_WS_URL", "wss://stream.binance.com:9443"),
		KrakenWSURL:        getEnv("KRAKEN_WS_URL", "wss://ws.kraken.com"),
		BybitWSURL:         getEnv("BYBIT_WS_URL", "wss://stream.bybit.com/v5/public/spot"),
		OKXWSURL:           getEnv("OKX_WS_URL", "wss://ws.okx.com:8443/ws/v5/public"),
		GateWSURL:          getEnv("GATE_WS_URL", "wss://api.gateio.ws/ws/v4/"),
		MEXCWSURL:          getEnv("MEXC_WS_URL", "wss://wbs.mexc.com/ws"),
		BitgetWSURL:        getEnv("BITGET_WS_URL", "wss://ws.bitget.com/v2/ws/public"),
		HTXWSURL:           getEnv("HTX_WS_URL", "wss://api.huobi.pro/ws"),
		CryptoComWSURL:     getEnv("CRYPTOCOM_WS_URL", "wss://stream.crypto.com/exchange/v1/market"),
		KuCoinAPIURL:       getEnv("KUCOIN_API_URL", "https://api.kucoin.com"),
		MinNetProfitPct:    getEnvFloat("MIN_NET_PROFIT_PCT", 0.0015),
		MaxPositionUSDT:    getEnvFloat("MAX_POSITION_USDT", 1000),
		ExecutionInterval:  time.Duration(getEnvInt("EXECUTION_INTERVAL_MS", 100)) * time.Millisecond,
		OpportunityTTL:     time.Duration(getEnvInt("OPPORTUNITY_TTL_MS", 500)) * time.Millisecond,
		StalenessThreshold: time.Duration(getEnvInt("STALENESS_THRESHOLD_MS", 2000)) * time.Millisecond,
		WindowSize:         getEnvInt("WINDOW_SIZE", 500),
		MinSamples:         getEnvInt("MIN_SAMPLES", 100),
		CircuitBreakerN:    getEnvInt("CIRCUIT_BREAKER_N", 5),
		CircuitBreakerLossPct:   getEnvFloat("CIRCUIT_BREAKER_LOSS_PCT", -0.005),
		CircuitBreakerPauseMins: getEnvInt("CIRCUIT_BREAKER_PAUSE_MINUTES", 5),
		InitialUSDTPerExchange: getEnvFloat("INITIAL_USDT_PER_EXCHANGE", 10000),
		InitialBTCPerExchange:  getEnvFloat("INITIAL_BTC_PER_EXCHANGE", 0.1),
		AllowedOrigin:      getEnv("ALLOWED_ORIGIN", "http://localhost:3000"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
