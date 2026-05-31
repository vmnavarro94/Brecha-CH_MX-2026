package exchange

import (
	"testing"
)

// TestBinance_ParseBookTicker verifies the combined-stream wrapped bookTicker frame parse.
// Frame format: {"stream":"btcusdt@bookTicker","data":{...}}
func TestBinance_ParseBookTicker(t *testing.T) {
	b := NewBinance("ws://test", nil)
	msg := []byte(`{"stream":"btcusdt@bookTicker","data":{"u":400900217,"s":"BTCUSDT","b":"73000.50","B":"1.234","a":"73001.20","A":"2.345"}}`)
	pu, ok := b.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed wrapped bookTicker frame")
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
	b := NewBinance("ws://test", nil)
	if _, ok := b.parseMessage([]byte(`not json`)); ok {
		t.Error("parseMessage should reject non-JSON")
	}
	if _, ok := b.parseMessage([]byte(`{"stream":"btcusdt@bookTicker","data":{"b":"not-a-number","a":"73001.20"}}`)); ok {
		t.Error("parseMessage should reject non-numeric bid in wrapped frame")
	}
}

// TestBinance_ParseDepth20 verifies that a combined-stream depth20@100ms frame
// produces a BookUpdate with non-empty Bids and Asks. Spec: D3.
func TestBinance_ParseDepth20(t *testing.T) {
	b := NewBinance("ws://test", nil)
	msg := []byte(`{
		"stream":"btcusdt@depth20@100ms",
		"data":{
			"lastUpdateId":160,
			"bids":[["0.0024","10"],["0.0023","5"]],
			"asks":[["0.0026","100"],["0.0027","200"]]
		}
	}`)
	bu, ok := b.parseDepth20(msg)
	if !ok {
		t.Fatal("parseDepth20 should accept a well-formed depth20 frame")
	}
	if bu.Exchange != "binance" {
		t.Errorf("Exchange: got %q, want binance", bu.Exchange)
	}
	if len(bu.Bids) != 2 {
		t.Errorf("Bids: got %d levels, want 2", len(bu.Bids))
	}
	if len(bu.Asks) != 2 {
		t.Errorf("Asks: got %d levels, want 2", len(bu.Asks))
	}
	if bu.Bids[0].Price.String() != "0.0024" {
		t.Errorf("Bids[0].Price: got %s, want 0.0024", bu.Bids[0].Price.String())
	}
	if bu.Asks[0].Price.String() != "0.0026" {
		t.Errorf("Asks[0].Price: got %s, want 0.0026", bu.Asks[0].Price.String())
	}
}

// TestBybit_ParseSnapshot verifies the orderbook.50 snapshot frame parses BBO
// and emits a BookUpdate with up to 50 levels. Spec: D4.
func TestBybit_ParseSnapshot(t *testing.T) {
	by := NewBybit("ws://test", nil)
	msg := []byte(`{
		"topic":"orderbook.50.BTCUSDT",
		"type":"snapshot",
		"data":{
			"s":"BTCUSDT",
			"b":[["73010.50","0.5"],["73010.00","1.0"]],
			"a":[["73011.00","1.2"],["73011.50","0.8"]]
		}
	}`)
	pu, ok := by.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed bybit orderbook.50 snapshot frame")
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

// TestBybit_ParseSnapshot_BookUpdate verifies that a snapshot frame produces a BookUpdate
// via parseSnapshot with non-empty Bids and Asks. Spec: D4.
func TestBybit_ParseSnapshot_BookUpdate(t *testing.T) {
	by := NewBybit("ws://test", nil)
	msg := []byte(`{
		"topic":"orderbook.50.BTCUSDT",
		"type":"snapshot",
		"data":{
			"s":"BTCUSDT",
			"b":[["73010.50","0.5"],["73010.00","1.0"]],
			"a":[["73011.00","1.2"],["73011.50","0.8"]]
		}
	}`)
	bu, ok := by.parseSnapshot(msg)
	if !ok {
		t.Fatal("parseSnapshot should accept a well-formed snapshot frame")
	}
	if bu.Exchange != "bybit" {
		t.Errorf("Exchange: got %q, want bybit", bu.Exchange)
	}
	if len(bu.Bids) != 2 {
		t.Errorf("Bids: got %d levels, want 2", len(bu.Bids))
	}
	if len(bu.Asks) != 2 {
		t.Errorf("Asks: got %d levels, want 2", len(bu.Asks))
	}
}

// TestBybit_ParseDelta_Ignored verifies that delta frames do not produce a BookUpdate. Spec: D4.
func TestBybit_ParseDelta_Ignored(t *testing.T) {
	by := NewBybit("ws://test", nil)
	msg := []byte(`{
		"topic":"orderbook.50.BTCUSDT",
		"type":"delta",
		"data":{
			"s":"BTCUSDT",
			"b":[["73010.50","0.5"]],
			"a":[]
		}
	}`)
	_, ok := by.parseSnapshot(msg)
	if ok {
		t.Error("parseSnapshot should not emit a BookUpdate for delta frames")
	}
}

func TestBybitParseMessage_Empty(t *testing.T) {
	by := NewBybit("ws://test", nil)
	if _, ok := by.parseMessage([]byte(`{"topic":"orderbook.50.BTCUSDT","type":"snapshot","data":{"b":[],"a":[]}}`)); ok {
		t.Error("parseMessage should reject snapshot frame with no bids or asks")
	}
}

// TestOKXParseMessage verifies the tickers frame parse.
func TestOKXParseMessage(t *testing.T) {
	o := NewOKX("ws://test", nil)
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
	o := NewOKX("ws://test", nil)
	if _, ok := o.parseMessage([]byte(`{"event":"subscribe","arg":{"channel":"tickers"}}`)); ok {
		t.Error("parseMessage should ignore subscribe-ack events")
	}
}

// TestOKX_ParseBooks5 verifies that a books5 frame produces a BookUpdate with 5 levels.
// Spec: D1 (OKX books5).
func TestOKX_ParseBooks5(t *testing.T) {
	o := NewOKX("ws://test", nil)
	msg := []byte(`{
		"arg":{"channel":"books5","instId":"BTC-USDT"},
		"data":[{
			"bids":[["73019.5","0.5","0","1"],["73019.0","1.0","0","2"],["73018.5","0.3","0","1"],["73018.0","0.8","0","2"],["73017.5","0.2","0","1"]],
			"asks":[["73020.0","0.4","0","1"],["73020.5","0.9","0","2"],["73021.0","0.1","0","1"],["73021.5","0.6","0","2"],["73022.0","0.3","0","1"]],
			"instId":"BTC-USDT",
			"ts":"1597026383085",
			"checksum":-855196043
		}]
	}`)
	bu, ok := o.parseBooks5(msg)
	if !ok {
		t.Fatal("parseBooks5 should accept a well-formed books5 frame")
	}
	if bu.Exchange != "okx" {
		t.Errorf("Exchange: got %q, want okx", bu.Exchange)
	}
	if len(bu.Bids) != 5 {
		t.Errorf("Bids: got %d levels, want 5", len(bu.Bids))
	}
	if len(bu.Asks) != 5 {
		t.Errorf("Asks: got %d levels, want 5", len(bu.Asks))
	}
	if bu.Bids[0].Price.String() != "73019.5" {
		t.Errorf("Bids[0].Price: got %s, want 73019.5", bu.Bids[0].Price.String())
	}
	if bu.Asks[0].Price.String() != "73020" {
		t.Errorf("Asks[0].Price: got %s, want 73020", bu.Asks[0].Price.String())
	}
}

// TestKrakenParseMessage verifies the legacy v1 ticker array-frame parse.
// Format: [channelID, {ticker}, "ticker", "XBT/USDT"]
// ticker.b = [price, wholeLotVolume, lotVolume]
func TestKrakenParseMessage(t *testing.T) {
	k := NewKraken("ws://test", nil)
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
	k := NewKraken("ws://test", nil)
	if _, ok := k.parseMessage([]byte(`{"event":"heartbeat"}`)); ok {
		t.Error("parseMessage should ignore non-array (event) frames")
	}
}

// TestGateParseMessage verifies Gate.io spot.book_ticker frame parse.
func TestGateParseMessage(t *testing.T) {
	g := NewGate("ws://test", nil)
	msg := []byte(`{"channel":"spot.book_ticker","event":"update","result":{"b":"73008.50","B":"0.5","a":"73009.10","A":"0.6"}}`)
	pu, ok := g.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed gate frame")
	}
	if pu.Exchange != "gate" || pu.Bid.String() != "73008.5" || pu.Ask.String() != "73009.1" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestGateParseMessage_WrongChannel(t *testing.T) {
	g := NewGate("ws://test", nil)
	if _, ok := g.parseMessage([]byte(`{"channel":"spot.trades","event":"update"}`)); ok {
		t.Error("parseMessage should reject non book_ticker channel")
	}
}

// TestMEXCParseMessage verifies MEXC bookTicker v3 frame parse.
func TestMEXCParseMessage(t *testing.T) {
	m := NewMEXC("ws://test", nil)
	msg := []byte(`{"c":"spot@public.bookTicker.v3.api@BTCUSDT","d":{"b":"73005.00","B":"0.1","a":"73005.50","A":"0.2"}}`)
	pu, ok := m.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed mexc frame")
	}
	if pu.Exchange != "mexc" || pu.Bid.String() != "73005" || pu.Ask.String() != "73005.5" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestMEXCParseMessage_Ping(t *testing.T) {
	m := NewMEXC("ws://test", nil)
	if _, ok := m.parseMessage([]byte(`{"msg":"PING"}`)); ok {
		t.Error("parseMessage should not produce a PriceUpdate from a PING frame")
	}
}

// TestBitgetParseMessage verifies Bitget books1 snapshot parse.
func TestBitgetParseMessage(t *testing.T) {
	b := NewBitget("ws://test", nil)
	msg := []byte(`{"action":"snapshot","arg":{"channel":"books1"},"data":[{"asks":[["73020.00","0.4"]],"bids":[["73019.50","0.3"]],"ts":"123"}]}`)
	pu, ok := b.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed bitget frame")
	}
	if pu.Exchange != "bitget" || pu.Bid.String() != "73019.5" || pu.Ask.String() != "73020" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestBitgetParseMessage_NonBooks(t *testing.T) {
	b := NewBitget("ws://test", nil)
	if _, ok := b.parseMessage([]byte(`{"arg":{"channel":"trades"},"data":[]}`)); ok {
		t.Error("parseMessage should reject non-books1 channel")
	}
}

// TestHTXParseMessage verifies HTX bbo frame parse (decompressed by caller).
func TestHTXParseMessage(t *testing.T) {
	h := NewHTX("ws://test", nil)
	msg := []byte(`{"ch":"market.btcusdt.bbo","tick":{"bid":73015.5,"bidSize":0.7,"ask":73016.2,"askSize":0.8}}`)
	pu, ok := h.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed htx frame")
	}
	if pu.Exchange != "htx" || pu.Bid.String() != "73015.5" || pu.Ask.String() != "73016.2" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestHTXParseMessage_Ping(t *testing.T) {
	h := NewHTX("ws://test", nil)
	if _, ok := h.parseMessage([]byte(`{"ping":1234567890}`)); ok {
		t.Error("parseMessage should not emit on a ping frame")
	}
}

// TestCryptoComParseMessage verifies Crypto.com ticker frame parse.
func TestCryptoComParseMessage(t *testing.T) {
	c := NewCryptoCom("ws://test", nil)
	msg := []byte(`{"id":1,"result":{"channel":"ticker","data":[{"b":"73010.00","bs":"0.5","k":"73010.80","ks":"0.7"}]}}`)
	pu, ok := c.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed cryptocom frame")
	}
	if pu.Exchange != "cryptocom" || pu.Bid.String() != "73010" || pu.Ask.String() != "73010.8" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestCryptoComParseMessage_Heartbeat(t *testing.T) {
	c := NewCryptoCom("ws://test", nil)
	if _, ok := c.parseMessage([]byte(`{"id":99,"method":"public/heartbeat"}`)); ok {
		t.Error("parseMessage should not emit on a heartbeat frame")
	}
}

// TestKuCoinParseMessage verifies KuCoin /market/ticker frame parse.
func TestKuCoinParseMessage(t *testing.T) {
	k := NewKuCoin("https://api.kucoin.com", nil)
	msg := []byte(`{"type":"message","topic":"/market/ticker:BTC-USDT","data":{"bestBid":"73004.10","bestBidSize":"0.2","bestAsk":"73004.90","bestAskSize":"0.3"}}`)
	pu, ok := k.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed kucoin frame")
	}
	if pu.Exchange != "kucoin" || pu.Bid.String() != "73004.1" || pu.Ask.String() != "73004.9" {
		t.Errorf("unexpected PriceUpdate: %+v", pu)
	}
}

func TestKuCoinParseMessage_Welcome(t *testing.T) {
	k := NewKuCoin("https://api.kucoin.com", nil)
	if _, ok := k.parseMessage([]byte(`{"type":"welcome","id":"abc"}`)); ok {
		t.Error("parseMessage should reject non-message type frames")
	}
}
