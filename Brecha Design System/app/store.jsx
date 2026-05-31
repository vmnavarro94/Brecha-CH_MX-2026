/* global React */
/* ============================================================
   Brecha — store + live-simulation engine
   Mirrors the documented backend: a mock WebSocket emits
   { type, data } envelopes (price_update / opportunity /
   trade_executed / pnl_update / circuit_breaker) plus REST-style
   spread polling. Money fields arrive as STRINGS exactly like the
   real API; reducers parseFloat them into the store.
   ============================================================ */

// ---------- tiny external store (Zustand-shaped, useSyncExternalStore) ----------
function createStore(initial) {
  let state = initial;
  const listeners = new Set();
  return {
    get: () => state,
    set(patch) {
      const next = typeof patch === "function" ? patch(state) : patch;
      state = Object.assign({}, state, next);
      listeners.forEach((l) => l());
    },
    subscribe(l) { listeners.add(l); return () => listeners.delete(l); },
  };
}

const EXCHANGES = ["binance", "kraken", "bybit"];
const FEATURED_PAIRS = ["binance-kraken", "binance-bybit", "kraken-bybit"];
const PAIR_COLOR = { "binance-kraken": "var(--orange)", "binance-bybit": "var(--info)", "kraken-bybit": "var(--up)" };

const store = createStore({
  prices: { binance: null, kraken: null, bybit: null },
  opportunities: [],
  trades: [],
  pnlHistory: [],
  spreads: [],
  zSeries: {},
  tradeMarks: {},
  circuitBreakerState: "active",
  pnl: { total_pnl: 0, trade_count: 0, win_rate: 0 },
  wsConnected: true,
  lastTradeId: null,
  lastOppId: null,
});

const actions = {
  setPrices(u) {
    store.set((s) => ({
      prices: { ...s.prices, [u.exchange]: { bid: parseFloat(u.bid), ask: parseFloat(u.ask), receivedAt: Date.now() } },
    }));
  },
  addOpportunity(o) {
    store.set((s) => {
      const parsed = {
        ID: o.ID, BuyExchange: o.BuyExchange, SellExchange: o.SellExchange,
        BuyPrice: parseFloat(o.BuyPrice), SellPrice: parseFloat(o.SellPrice),
        NetProfit: parseFloat(o.NetProfit), NetProfitPct: parseFloat(o.NetProfitPct),
        ZScore: parseFloat(o.ZScore), Score: parseFloat(o.Score), MaxVolume: parseFloat(o.MaxVolume),
        DetectedAt: o.DetectedAt, Status: o.Status, _t: Date.now(),
      };
      const next = [parsed, ...s.opportunities];
      if (next.length > 200) next.length = 200;
      return { opportunities: next, lastOppId: o.ID };
    });
  },
  addTrade(t) {
    store.set((s) => {
      const parsed = {
        ID: t.ID, OpportunityID: t.OpportunityID, BuyExchange: t.BuyExchange, SellExchange: t.SellExchange,
        BuyPrice: parseFloat(t.BuyPrice), SellPrice: parseFloat(t.SellPrice), Volume: parseFloat(t.Volume),
        GrossProfit: parseFloat(t.GrossProfit), Fees: parseFloat(t.Fees), NetProfit: parseFloat(t.NetProfit),
        Slippage: parseFloat(t.Slippage), ExecutedAt: t.ExecutedAt,
      };
      const pair = t.BuyExchange + "-" + t.SellExchange;
      const marks = { ...s.tradeMarks };
      if (FEATURED_PAIRS.includes(pair)) {
        const z = lastZ(s, pair);
        marks[pair] = [...(s.tradeMarks[pair] || []), { t: Date.now(), z, profit: parsed.NetProfit }].slice(-12);
      }
      return { trades: [parsed, ...s.trades], tradeMarks: marks, lastTradeId: t.ID };
    });
  },
  setTrades(list) { store.set({ trades: list }); },
  setPnL(p) {
    store.set((s) => {
      const value = parseFloat(p.total_pnl);
      return {
        pnl: { total_pnl: value, trade_count: p.trade_count, win_rate: p.win_rate },
        pnlHistory: [...s.pnlHistory, { time: Date.now(), value }],
      };
    });
  },
  setSpreads(list) {
    store.set((s) => {
      const now = Date.now();
      const zSeries = { ...s.zSeries };
      FEATURED_PAIRS.forEach((pair) => {
        const stat = list.find((x) => x.Pair === pair);
        if (!stat) return;
        const z = computeZ(s.prices, pair, stat);
        const arr = [...(zSeries[pair] || []), { t: now, z }].filter((p) => now - p.t < 60000);
        zSeries[pair] = arr;
      });
      return { spreads: list, zSeries };
    });
  },
  setCircuitBreakerState(state) { store.set({ circuitBreakerState: state }); },
  setWsConnected(c) { store.set({ wsConnected: c }); },
};

function lastZ(s, pair) { const a = s.zSeries[pair]; return a && a.length ? a[a.length - 1].z : 0; }

function computeZ(prices, pair, stat) {
  const [a, b] = pair.split("-");
  const pa = prices[a], pb = prices[b];
  if (!pa || !pb || !stat || stat.Std <= 0) return 0;
  const currentSpread = (pa.ask - pb.bid) / pa.ask;
  const z = (currentSpread - stat.Mean) / stat.Std;
  return Math.max(-3.6, Math.min(3.6, z));
}

function MockBackend(dispatch, getCfg) {
  let globalMid = 72000 + (Math.random() - 0.5) * 600;
  const offsets = { binance: 0, kraken: 4, bybit: -3 };
  const offsetTarget = { binance: 0, kraken: 4, bybit: -3 };
  const model = {};
  [...FEATURED_PAIRS, "kraken-binance", "bybit-binance", "bybit-kraken"].forEach((p, i) => {
    model[p] = {
      Pair: p,
      Mean: 0.00010 + (i % 3) * 0.00006,
      Std: 0.00026 + (i % 2) * 0.00009,
      Samples: p === "binance-bybit" ? 64 : 300 + Math.floor(Math.random() * 120),
    };
  });

  let counter = 91000;
  const timers = [];

  function priceFor(ex) {
    const mid = globalMid * (1 + offsets[ex] / 10000);
    const halfBps = 0.5 + Math.random() * 1.1;
    const half = mid * (halfBps / 10000);
    return { bid: (mid - half).toFixed(2), ask: (mid + half).toFixed(2) };
  }

  function tickPrices() {
    globalMid += (Math.random() - 0.5) * 22;
    globalMid = Math.max(60000, Math.min(85000, globalMid));
    EXCHANGES.forEach((ex) => {
      offsetTarget[ex] += (Math.random() - 0.5) * 1.4;
      offsetTarget[ex] = Math.max(-14, Math.min(14, offsetTarget[ex]));
      offsets[ex] += (offsetTarget[ex] - offsets[ex]) * 0.18;
      const p = priceFor(ex);
      dispatch({ type: "price_update", data: { exchange: ex, bid: p.bid, ask: p.ask } });
    });
  }

  function pollSpreads() {
    Object.values(model).forEach((m) => { if (m.Samples < 500) m.Samples = Math.min(500, m.Samples + 2 + Math.floor(Math.random() * 3)); });
    dispatch({ type: "spread_stats", data: Object.values(model).map((m) => ({ ...m })) });
  }

  function spawnOpp() {
    const cb = getCfg().circuitBreaker;
    const s = store.get();
    let pair = FEATURED_PAIRS[Math.floor(Math.random() * FEATURED_PAIRS.length)];
    const hot = FEATURED_PAIRS.filter((p) => Math.abs(lastZ(s, p)) > 1.8 && model[p].Samples >= 100);
    if (hot.length && Math.random() < 0.7) pair = hot[Math.floor(Math.random() * hot.length)];
    const [buy, sell] = pair.split("-");
    const m = model[pair];
    const z = lastZ(s, pair) || (Math.random() * 3 - 0.5);
    const buyP = (s.prices[buy] && s.prices[buy].ask) || globalMid;
    const sellP = (s.prices[sell] && s.prices[sell].bid) || globalMid * 1.0008;
    const spreadAbs = Math.max(0, sellP - buyP);
    // net % after fees — scales with the statistical anomaly so hot z-scores pay more
    const netPct = Math.max(0.0004, Math.min(0.0062,
      0.0003 + Math.abs(z) * 0.00055 + Math.random() * 0.0013 + (spreadAbs / buyP) * 0.4));
    const vol = 0.004 + Math.random() * 0.02;
    const net = netPct * buyP * vol * 14;
    const score = Math.max(0.05, Math.min(0.99, 0.32 * Math.min(1, Math.abs(z) / 3) + 0.5 * Math.min(1, netPct / 0.0025) + Math.random() * 0.18));

    let status;
    const ready = m.Samples >= 100;
    if (cb === "paused") status = Math.random() < 0.7 ? "skipped" : "expired";
    else if (!ready) status = Math.random() < 0.6 ? "skipped" : "detected";
    else if (score > 0.6 && Math.abs(z) > 1.6) status = Math.random() < 0.82 ? "executed" : "expired";
    else { const r = Math.random(); status = r < 0.4 ? "executed" : r < 0.72 ? "skipped" : r < 0.9 ? "expired" : "detected"; }
    if (cb === "watching" && status === "executed" && Math.random() < 0.4) status = "skipped";

    const id = uuid();
    dispatch({
      type: "opportunity",
      data: {
        ID: id, BuyExchange: buy, SellExchange: sell,
        BuyPrice: buyP.toFixed(2), SellPrice: Math.max(sellP, buyP + 1).toFixed(2),
        NetProfit: net.toFixed(2), NetProfitPct: netPct.toFixed(5),
        ZScore: z.toFixed(2), Score: score.toFixed(2), MaxVolume: (vol * 30).toFixed(2),
        DetectedAt: new Date().toISOString(), Status: status,
      },
    });

    if (status === "executed") {
      const delay = 260 + Math.random() * 360;
      timers.push(setTimeout(() => {
        const gross = net + 0.6 + Math.random() * 1.4;
        const fees = gross - net;
        const slip = Math.random() * 0.4;
        const loss = Math.random() < 0.08;
        const netFinal = loss ? -(0.1 + Math.random() * 0.8) : net;
        dispatch({
          type: "trade_executed",
          data: {
            ID: "TX-" + (counter++), OpportunityID: id, BuyExchange: buy, SellExchange: sell,
            BuyPrice: buyP.toFixed(2), SellPrice: Math.max(sellP, buyP + 1).toFixed(2),
            Volume: vol.toFixed(8), GrossProfit: gross.toFixed(2), Fees: fees.toFixed(2),
            NetProfit: netFinal.toFixed(2), Slippage: slip.toFixed(2), ExecutedAt: new Date().toISOString(),
          },
        });
      }, delay));
    }
  }

  function reschedule() {
    timers.forEach((t) => { clearInterval(t); clearTimeout(t); });
    timers.length = 0;
    const rate = getCfg().feedRate;
    timers.push(setInterval(tickPrices, 250));
    timers.push(setInterval(pollSpreads, 1000));
    const oppEvery = Math.max(650, 2200 / rate);
    timers.push(setInterval(spawnOpp, oppEvery));
  }

  return {
    start() { tickPrices(); pollSpreads(); reschedule(); },
    setRate() { reschedule(); },
    stop() { timers.forEach((t) => { clearInterval(t); clearTimeout(t); }); timers.length = 0; },
  };
}

function dispatchEvent(event) {
  switch (event.type) {
    case "price_update":    actions.setPrices(event.data); break;
    case "opportunity":     actions.addOpportunity(event.data); break;
    case "trade_executed":  actions.addTrade(event.data); recomputePnL(); break;
    case "pnl_update":      actions.setPnL(event.data); break;
    case "circuit_breaker": actions.setCircuitBreakerState(event.data.state); break;
    case "spread_stats":    actions.setSpreads(event.data); break;
  }
}

function recomputePnL() {
  const s = store.get();
  const total = s.trades.reduce((a, t) => a + t.NetProfit, 0);
  const wins = s.trades.filter((t) => t.NetProfit > 0).length;
  const win_rate = s.trades.length ? wins / s.trades.length : 0;
  actions.setPnL({ total_pnl: total.toFixed(2), trade_count: s.trades.length, win_rate });
}

function uuid() {
  return "xxxxxxxx-xxxx-4xxx-yxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0; const v = c === "x" ? r : (r & 0x3) | 0x8; return v.toString(16);
  });
}

function seedInitial() {
  const routes = [
    ["binance", "kraken"], ["kraken", "bybit"], ["binance", "bybit"],
    ["bybit", "binance"], ["kraken", "binance"], ["bybit", "kraken"],
  ];
  const now = Date.now();
  const trades = [];
  let counter = 90974;
  const N = 26;
  let tt = now - N * 16000;           // start ~N*16s in the past
  for (let k = 0; k < N; k++) {
    tt += 7000 + Math.random() * 16000; // strictly increasing timestamps
    const ts = Math.min(tt, now - 500);
    const [buy, sell] = routes[Math.floor(Math.random() * routes.length)];
    const base = 71400 + Math.random() * 900;
    const vol = (0.004 + Math.random() * 0.02);
    const loss = Math.random() < 0.1;
    const net = loss ? -(0.1 + Math.random() * 0.7) : (0.6 + Math.random() * 5.2);
    const fees = 0.8 + Math.random() * 1.6;
    trades.push({
      ID: "TX-" + (counter++), OpportunityID: uuid(), BuyExchange: buy, SellExchange: sell,
      BuyPrice: base.toFixed(2), SellPrice: (base * (1 + (12 + Math.random() * 50) / 10000)).toFixed(2),
      Volume: vol.toFixed(8), GrossProfit: (net + fees).toFixed(2), Fees: fees.toFixed(2),
      NetProfit: net.toFixed(2), Slippage: (Math.random() * 0.4).toFixed(2),
      ExecutedAt: new Date(ts).toISOString(),
    });
  }
  const parsed = trades.map((t) => ({
    ID: t.ID, OpportunityID: t.OpportunityID, BuyExchange: t.BuyExchange, SellExchange: t.SellExchange,
    BuyPrice: +t.BuyPrice, SellPrice: +t.SellPrice, Volume: +t.Volume, GrossProfit: +t.GrossProfit,
    Fees: +t.Fees, NetProfit: +t.NetProfit, Slippage: +t.Slippage, ExecutedAt: t.ExecutedAt,
  }));
  parsed.reverse();
  store.set({ trades: parsed });
  const chron = [...parsed].reverse();
  let cum = 0; const hist = chron.map((t) => { cum += t.NetProfit; return { time: new Date(t.ExecutedAt).getTime(), value: cum }; });
  store.set({ pnlHistory: hist });
  recomputePnL();
}

function useStore(selector) {
  return React.useSyncExternalStore(store.subscribe, () => selector(store.get()));
}

const fmtUsd = (n, dp = 2) => "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: dp, maximumFractionDigits: dp });
const fmtSigned = (n, dp = 2) => (n >= 0 ? "+" : "−") + "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: dp, maximumFractionDigits: dp });
const fmtPct = (frac) => (frac >= 0 ? "+" : "−") + (Math.abs(frac) * 100).toFixed(3) + "%";
const fmtTime = (iso) => { const d = new Date(iso); return d.toLocaleTimeString("es-MX", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }); };
const fmtHM = (ms) => new Date(ms).toLocaleTimeString("es-MX", { hour: "2-digit", minute: "2-digit", hour12: false });
const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1);

window.BX = {
  store, useStore, actions, dispatchEvent, MockBackend, seedInitial, recomputePnL,
  EXCHANGES, FEATURED_PAIRS, PAIR_COLOR, computeZ,
  fmtUsd, fmtSigned, fmtPct, fmtTime, fmtHM, cap,
};
