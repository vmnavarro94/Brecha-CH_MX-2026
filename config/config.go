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
	DataDir       string

	// DemoMode uses VIP-tier fees so the engine can detect opportunities with real
	// market data. Set DEMO_MODE=false (and raise MIN_NET_PROFIT_PCT) for production.
	DemoMode bool

	// Synthetic order book depth parameters.
	DepthLevels    int
	DepthStepPct   float64
	DepthMinQtyBTC float64
	DepthMaxQtyBTC float64

	// TriangularStrategy parameters.
	TriangularEnabled      bool
	TriangularNoiseRange   float64
	TriangularSeedRefPrice float64
	TriangularNotional     float64
	TriangularSeed         int64
	TriangularTakerFee     float64

	// FundingStrategy parameters.
	FundingEnabled          bool
	FundingThreshold        float64
	FundingPollInterval     time.Duration
	FundingBaseDifferential float64
	FundingNotional         float64
	FundingSeed             int64
	FundingEmitCooldown     time.Duration

	// Kelly + correlation + adaptive threshold parameters for SpatialStrategy.
	SpatialBaseMinNetProfitPct float64
	SpatialAdaptiveCoeff       float64
	KellyMinSamples            int
	KellyFraction              float64
	CorrPenaltyWeight          float64
	CorrWindowN                int
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
		AllowedOrigin:  getEnv("ALLOWED_ORIGIN", "http://localhost:3000"),
		DataDir:        getEnv("DATA_DIR", "./data"),
		DemoMode:       getEnvBool("DEMO_MODE", false),
		DepthLevels:    getEnvInt("DEPTH_LEVELS", 5),
		DepthStepPct:   getEnvFloat("DEPTH_STEP_PCT", 0.00005),
		DepthMinQtyBTC: getEnvFloat("DEPTH_MIN_QTY_BTC", 0.005),
		DepthMaxQtyBTC: getEnvFloat("DEPTH_MAX_QTY_BTC", 0.025),

		// Triangular strategy — off by default (out-of-spec bonus: 3-leg cycle
		// USDT→BTC→ETH→USDT introduces ETH and is outside the "BTC arbitrage"
		// mandate). Opt-in via TRIANGULAR_ENABLED=true.
		TriangularEnabled:      getEnvBool("TRIANGULAR_ENABLED", false),
		TriangularNoiseRange:   getEnvFloat("TRIANGULAR_NOISE_RANGE", 0.005),
		TriangularSeedRefPrice: getEnvFloat("TRIANGULAR_SEED_REF_PRICE", 2000.0),
		TriangularNotional:     getEnvFloat("TRIANGULAR_NOTIONAL", 1000.0),
		TriangularSeed:         int64(getEnvInt("TRIANGULAR_SEED", 42)),
		TriangularTakerFee:     getEnvFloat("TRIANGULAR_TAKER_FEE", 0.0001),

		// Funding strategy — demo defaults tuned to emit within 60s.
		FundingEnabled:          getEnvBool("FUNDING_ENABLED", true),
		FundingThreshold:        getEnvFloat("FUNDING_THRESHOLD", 0.0001),
		FundingPollInterval:     time.Duration(getEnvInt("FUNDING_POLL_INTERVAL_MS", 30000)) * time.Millisecond,
		FundingBaseDifferential: getEnvFloat("FUNDING_BASE_DIFFERENTIAL", 0.0003),
		FundingNotional:         getEnvFloat("FUNDING_NOTIONAL", 1000.0),
		FundingSeed:             int64(getEnvInt("FUNDING_SEED", 42)),
		FundingEmitCooldown:     time.Duration(getEnvInt("FUNDING_EMIT_COOLDOWN_MS", 5000)) * time.Millisecond,

		// Kelly + correlation + adaptive threshold (cold-start defaults match pre-change behavior).
		SpatialBaseMinNetProfitPct: getEnvFloat("SPATIAL_BASE_MIN_NET_PROFIT_PCT", 0.0),
		SpatialAdaptiveCoeff:       getEnvFloat("SPATIAL_ADAPTIVE_COEFF", 0.5),
		KellyMinSamples:            getEnvInt("KELLY_MIN_SAMPLES", 10),
		KellyFraction:              getEnvFloat("KELLY_FRACTION", 0.25),
		CorrPenaltyWeight:          getEnvFloat("CORR_PENALTY_WEIGHT", 0.3),
		CorrWindowN:                getEnvInt("CORR_WINDOW_N", 50),
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

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1"
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
