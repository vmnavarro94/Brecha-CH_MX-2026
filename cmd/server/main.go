package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/vmnavarro94/coding-challenge-mexico/config"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/exchange"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/feed"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	cfg := config.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	connectors := []exchange.Connector{
		exchange.NewBinance(cfg.BinanceWSURL),
		exchange.NewKraken(cfg.KrakenWSURL),
		exchange.NewBybit(cfg.BybitWSURL),
	}

	agg := feed.NewAggregator(connectors)
	if err := agg.Start(ctx); err != nil {
		slog.Error("failed to start aggregator", "err", err)
		os.Exit(1)
	}

	slog.Info("system started", "port", cfg.Port)

	// Temporary: log price updates to verify connectors are working.
	go func() {
		for update := range agg.Updates() {
			slog.Debug("price update",
				"exchange", update.Exchange,
				"bid", update.Bid,
				"ask", update.Ask,
			)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
}
