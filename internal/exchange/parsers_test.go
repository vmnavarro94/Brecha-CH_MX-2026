package exchange

import (
	"testing"
)

// TestBinanceParseMessage verifies the @bookTicker frame parse.
func TestBinanceParseMessage(t *testing.T) {
	b := NewBinance("ws://test")
	msg := []byte(`{"u":400900217,"s":"BTCUSDT","b":"73000.50","B":"1.234","a":"73001.20","A":"2.345"}`)
	pu, ok := b.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed bookTicker frame")
	}
	if pu.Exchange != "binance" {
		t.Errorf("Exchange: got %q, want binance", pu.Exchange)
	}
	if pu.Bid.String() != "73000.5" {
		t.Errorf("Bid: got %s, want 73000.5", pu.Bid.String())
	}
	if pu.Ask.String() != "73001.2" {
		t.Errorf("Ask: got %s, want 73001.2", pu.Ask.String())
	}
	if pu.BidSize.String() != "1.234" {
		t.Errorf("BidSize: got %s, want 1.234", pu.BidSize.String())
	}
}

func TestBinanceParseMessage_Garbage(t *testing.T) {
	b := NewBinance("ws://test")
	if _, ok := b.parseMessage([]byte(`not json`)); ok {
		t.Error("parseMessage should reject non-JSON")
	}
	if _, ok := b.parseMessage([]byte(`{"b":"not-a-number","a":"73001.20"}`)); ok {
		t.Error("parseMessage should reject non-numeric bid")
	}
}

// TestBybitParseMessage verifies the orderbook.1 frame parse.
func TestBybitParseMessage(t *testing.T) {
	by := NewBybit("ws://test")
	msg := []byte(`{
		"topic":"orderbook.1.BTCUSDT",
		"type":"snapshot",
		"data":{
			"s":"BTCUSDT",
			"b":[["73010.50","0.5"]],
			"a":[["73011.00","1.2"]]
		}
	}`)
	pu, ok := by.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed bybit frame")
	}
	if pu.Exchange != "bybit" {
		t.Errorf("Exchange: got %q, want bybit", pu.Exchange)
	}
	if pu.Bid.String() != "73010.5" {
		t.Errorf("Bid: got %s, want 73010.5", pu.Bid.String())
	}
	if pu.Ask.String() != "73011" {
		t.Errorf("Ask: got %s, want 73011", pu.Ask.String())
	}
}

func TestBybitParseMessage_Empty(t *testing.T) {
	by := NewBybit("ws://test")
	if _, ok := by.parseMessage([]byte(`{"topic":"orderbook.1.BTCUSDT","data":{"b":[],"a":[]}}`)); ok {
		t.Error("parseMessage should reject frame with no bids or asks")
	}
}

// TestOKXParseMessage verifies the tickers frame parse.
func TestOKXParseMessage(t *testing.T) {
	o := NewOKX("ws://test")
	msg := []byte(`{
		"arg":{"channel":"tickers","instId":"BTC-USDT"},
		"data":[{
			"instId":"BTC-USDT",
			"bidPx":"73019.50",
			"bidSz":"0.7",
			"askPx":"73020.00",
			"askSz":"0.3"
		}]
	}`)
	pu, ok := o.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed OKX tickers frame")
	}
	if pu.Exchange != "okx" {
		t.Errorf("Exchange: got %q, want okx", pu.Exchange)
	}
	if pu.Bid.String() != "73019.5" {
		t.Errorf("Bid: got %s, want 73019.5", pu.Bid.String())
	}
	if pu.Ask.String() != "73020" {
		t.Errorf("Ask: got %s, want 73020", pu.Ask.String())
	}
}

func TestOKXParseMessage_SubscribeAck(t *testing.T) {
	o := NewOKX("ws://test")
	if _, ok := o.parseMessage([]byte(`{"event":"subscribe","arg":{"channel":"tickers"}}`)); ok {
		t.Error("parseMessage should ignore subscribe-ack events")
	}
}

// TestKrakenParseMessage verifies the legacy v1 ticker array-frame parse.
// Format: [channelID, {ticker}, "ticker", "XBT/USDT"]
// ticker.b = [price, wholeLotVolume, lotVolume]
func TestKrakenParseMessage(t *testing.T) {
	k := NewKraken("ws://test")
	msg := []byte(`[123,{"b":["73015.30","1","0.8"],"a":["73016.10","2","1.5"]},"ticker","XBT/USDT"]`)
	pu, ok := k.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed kraken ticker frame")
	}
	if pu.Exchange != "kraken" {
		t.Errorf("Exchange: got %q, want kraken", pu.Exchange)
	}
	if pu.Bid.String() != "73015.3" {
		t.Errorf("Bid: got %s, want 73015.3", pu.Bid.String())
	}
	if pu.Ask.String() != "73016.1" {
		t.Errorf("Ask: got %s, want 73016.1", pu.Ask.String())
	}
}

func TestKrakenParseMessage_Heartbeat(t *testing.T) {
	k := NewKraken("ws://test")
	if _, ok := k.parseMessage([]byte(`{"event":"heartbeat"}`)); ok {
		t.Error("parseMessage should ignore non-array (event) frames")
	}
}
