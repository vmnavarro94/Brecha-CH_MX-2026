/* global React, Icon */
// Brecha — Handoff: controlled, static renderers of each component state.
// Reuses the .bx-* classes from styles.css for pixel-fidelity to the prototype.

const hUsd = (n, dp = 2) => "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: dp, maximumFractionDigits: dp });
const hSigned = (n, dp = 2) => (n >= 0 ? "+" : "−") + "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: dp, maximumFractionDigits: dp });
const hPct = (f) => (f >= 0 ? "+" : "−") + (Math.abs(f) * 100).toFixed(3) + "%";
const hCap = (s) => s.charAt(0).toUpperCase() + s.slice(1);

const CB = {
  active:   { cls: "bx-cb-active",   label: "ACTIVO" },
  watching: { cls: "bx-cb-watching", label: "VIGILANDO" },
  paused:   { cls: "bx-cb-paused",   label: "PAUSADO" },
};

// ---------------- Status Bar (controlled) ----------------
function HStatusBar({ cb = "active", pnl = 124.5, winRate = 0.714, trades = 42, exec = 3, ws = true }) {
  const info = CB[cb];
  const cls = pnl > 0 ? "is-up" : pnl < 0 ? "is-down" : "is-flat";
  const sign = pnl === 0;
  return (
    <header className="bx-status" style={{ width: "100%", borderRadius: "var(--r-md)", border: "1px solid var(--line)" }}>
      <div className="bx-status-brand">
        <div className="bx-mark"><span className="b1" /><span className="b2" /></div>
        <div><div className="bx-word">brecha</div><div className="bx-word-sub">Arbitrage Engine</div></div>
      </div>
      <div className="bx-cb">
        <span className="bx-cb-k">Circuit Breaker</span>
        <span className={"bx-cb-pill " + info.cls}><span className="bx-cb-dot" />{info.label}</span>
      </div>
      <div className="bx-pnl-hero">
        <span className="bx-pnl-hero-k">P&amp;L acumulado · sesión</span>
        <span className={"bx-pnl-hero-v " + cls}>{sign ? "$0.00" : hSigned(pnl)}</span>
        <span className="bx-pnl-hero-sub"><Icon name="trending-up" size={13} className={cls} />{trades} trades · {exec} ejecutadas en vivo</span>
      </div>
      <div className="bx-stat-grp">
        <div className="bx-stat"><span className="bx-stat-k">Tasa de acierto</span><span className="bx-stat-v">{(winRate * 100).toFixed(1)}%</span></div>
        <div className="bx-stat"><span className="bx-stat-k">Exchanges</span><span className="bx-stat-v">3<small> / 3</small></span></div>
        <div className="bx-stat"><span className="bx-stat-k">Trades</span><span className="bx-stat-v">{trades}</span></div>
      </div>
      <div className={"bx-ws " + (ws ? "up" : "down")}><span className="bx-ws-dot" /><span className="bx-ws-label">{ws ? "Connected" : "Reconnecting…"}</span></div>
    </header>
  );
}

// ---------------- Price row (controlled) ----------------
const EX_META = { binance: { label: "Binance", color: "#F3BA2F" }, kraken: { label: "Kraken", color: "#7B68EE" }, bybit: { label: "Bybit", color: "#F7A600" } };
function HPriceTable({ rows }) {
  return (
    <div className="bx-panel bx-prices" style={{ width: "100%" }}>
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="radio" size={13} /> Precios en vivo · BBO</span>
        <span className="bx-head-meta">BTC/USDT · 250 ms</span>
      </div>
      <table className="bx-ptable">
        <thead><tr><th scope="col">Exchange</th><th scope="col">Bid</th><th scope="col">Ask</th><th scope="col">Spread</th><th scope="col">Estado</th></tr></thead>
        <tbody>
          {rows.map((r, i) => {
            const meta = EX_META[r.ex];
            const stCls = r.status === "live" ? "bx-px-live" : r.status === "stale" ? "bx-px-stale" : "bx-px-wait";
            const stLbl = r.status === "live" ? "Live" : r.status === "stale" ? "Stale" : "Waiting…";
            return (
              <tr key={i}>
                <td><div className="bx-px-ex"><span className="bx-px-badge" style={{ background: meta.color + "22", color: meta.color }}>{meta.label[0]}</span><span className="bx-px-name">{meta.label}</span></div></td>
                <td><span className={"bx-px-cell bx-px-bid " + (r.flashBid || "")}>{r.bid != null ? r.bid.toFixed(2) : "—"}</span></td>
                <td><span className={"bx-px-cell bx-px-ask " + (r.flashAsk || "")}>{r.ask != null ? r.ask.toFixed(2) : "—"}</span></td>
                <td><span className="bx-px-spread num">{r.bid != null ? ((r.ask - r.bid) / r.ask * 100).toFixed(4) + "%" : "—"}</span></td>
                <td><span className={"bx-px-status " + stCls}><span className="d" />{stLbl}</span></td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ---------------- Spread chart mini (controlled) ----------------
function HSpreadMini({ series = [], color = "var(--orange)", warming = false, samples = 64, anomaly = false, marks = [] }) {
  const W = 360, H = 150, PADL = 24, PADR = 10, PADT = 10, PADB = 12;
  const Z_MIN = -3.5, Z_MAX = 3.5, plotW = W - PADL - PADR, plotH = H - PADT - PADB;
  const x = (i) => PADL + (i / (series.length - 1 || 1)) * plotW;
  const y = (z) => PADT + (1 - (z - Z_MIN) / (Z_MAX - Z_MIN)) * plotH;
  const line = series.map((z, i) => (i ? "L" : "M") + x(i).toFixed(1) + " " + y(z).toFixed(1)).join(" ");
  const refs = [{ z: 2, c: "var(--orange-line)", d: "3 4", l: "+2σ" }, { z: 1, c: "rgba(255,197,61,0.28)", d: "2 5", l: "+1σ" }, { z: 0, c: "var(--line)", d: "", l: "0" }, { z: -1, c: "rgba(255,197,61,0.28)", d: "2 5", l: "−1σ" }, { z: -2, c: "var(--orange-line)", d: "3 4", l: "−2σ" }];
  return (
    <div style={{ position: "relative", width: "100%" }}>
      {warming && <div className="bx-warming" style={{ top: 8 }}><Icon name="loader" size={12} />Calentando modelo: {samples}/100 samples<span className="bar"><span style={{ width: samples + "%" }} /></span></div>}
      <svg className="ho-mini-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
        <rect x={PADL} y={y(Z_MAX)} width={plotW} height={y(2) - y(Z_MAX)} fill="var(--orange-wash)" opacity={anomaly ? 0.7 : 0.35} />
        <rect x={PADL} y={y(-2)} width={plotW} height={y(Z_MIN) - y(-2)} fill="var(--orange-wash)" opacity="0.35" />
        {refs.map((r) => <line key={r.z} x1={PADL} y1={y(r.z)} x2={W - PADR} y2={y(r.z)} stroke={r.c} strokeWidth="1" strokeDasharray={r.d} vectorEffect="non-scaling-stroke" />)}
        {series.length > 1 && <path d={line} fill="none" stroke={color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" strokeLinejoin="round" opacity={warming ? 0.4 : 1} />}
        {marks.map((m, i) => <g key={i}><circle cx={x(m.i)} cy={y(m.z)} r="6" fill="none" stroke={m.win ? "var(--up)" : "var(--down)"} strokeWidth="1.4" vectorEffect="non-scaling-stroke" /><circle cx={x(m.i)} cy={y(m.z)} r="2.4" fill={m.win ? "var(--up)" : "var(--down)"} /></g>)}
      </svg>
    </div>
  );
}

// ---------------- PnL chart mini (controlled) ----------------
function HPnlMini({ data = null, tooltip = false }) {
  const W = 360, H = 150, PADL = 30, PADR = 8, PADT = 10, PADB = 14;
  if (!data) return <div className="ho-mini-chart" style={{ position: "relative" }}><div className="bx-pnl-empty"><Icon name="hourglass" size={20} />Esperando primer trade…</div></div>;
  const positive = data[data.length - 1] >= 0;
  const color = positive ? "var(--up)" : "var(--down)";
  const plotW = W - PADL - PADR, plotH = H - PADT - PADB;
  const vmax = Math.max(0, ...data), vmin = Math.min(0, ...data), pad = (vmax - vmin) * 0.12 || 1, lo = vmin - pad, hi = vmax + pad;
  const x = (i) => PADL + (i / (data.length - 1)) * plotW;
  const y = (v) => PADT + (1 - (v - lo) / (hi - lo)) * plotH;
  const pts = data.map((v, i) => [x(i), y(v)]);
  const lineP = pts.map((p, i) => (i ? "L" : "M") + p[0].toFixed(1) + " " + p[1].toFixed(1)).join(" ");
  const y0 = y(0);
  const area = lineP + ` L ${pts[pts.length - 1][0].toFixed(1)} ${y0.toFixed(1)} L ${pts[0][0].toFixed(1)} ${y0.toFixed(1)} Z`;
  const hi2 = Math.round(data.length * 0.66);
  return (
    <div style={{ position: "relative", width: "100%" }}>
      <svg className="ho-mini-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
        <defs><linearGradient id={"hg" + (positive ? "p" : "n")} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor={color} stopOpacity="0.3" /><stop offset="100%" stopColor={color} stopOpacity="0" /></linearGradient></defs>
        <line x1={PADL} y1={y0} x2={W - PADR} y2={y0} stroke="var(--line)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        <path d={area} fill={`url(#hg${positive ? "p" : "n"})`} />
        <path d={lineP} fill="none" stroke={color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
        {tooltip && <line x1={x(hi2)} y1={PADT} x2={x(hi2)} y2={PADT + plotH} stroke="var(--line-strong)" strokeWidth="1" strokeDasharray="2 3" vectorEffect="non-scaling-stroke" />}
        {tooltip && <circle cx={x(hi2)} cy={y(data[hi2])} r="4" fill={color} stroke="var(--bg-surface)" strokeWidth="1.5" />}
      </svg>
      {tooltip && <div className="bx-pnl-tip" style={{ left: `${x(hi2) / W * 100}%`, top: `${y(data[hi2]) / H * 100}%` }}><div className="v is-up">{hSigned(data[hi2])}</div><div className="t">14:21:08</div></div>}
    </div>
  );
}

// ---------------- Opportunity row (controlled) ----------------
const ST = { executed: { c: "bx-st-executed", i: "check", l: "Ejecutada" }, skipped: { c: "bx-st-skipped", i: "minus", l: "Descartada" }, expired: { c: "bx-st-expired", i: "clock", l: "Expirada" }, detected: { c: "bx-st-detected", i: "search", l: "Detectada" } };
function HOppRow({ op }) {
  const st = ST[op.status]; const hot = Math.abs(op.z) > 2;
  return (
    <div className="bx-opp">
      <div className="bx-opp-top">
        <span className="bx-opp-route"><b>{hCap(op.buy)}</b><Icon name="arrow-right" size={13} /><b>{hCap(op.sell)}</b></span>
        <span className={"bx-stbadge " + st.c}><Icon name={st.i} size={11} />{st.l}</span>
        <span className="bx-opp-time">{op.t}</span>
      </div>
      <div className="bx-opp-bottom">
        <span className="bx-opp-pct is-up">{hPct(op.pct)}</span>
        <span className="bx-opp-zwrap">z<span className={"bx-opp-zbadge" + (hot ? " hot" : "")}>{op.z >= 0 ? "+" : "−"}{Math.abs(op.z).toFixed(2)}</span></span>
        <span className="bx-opp-score"><span className="bx-opp-scorebar"><span style={{ width: (op.score * 100) + "%" }} /></span><span className="bx-opp-scoreval">{op.score.toFixed(2)}</span></span>
      </div>
    </div>
  );
}

Object.assign(window, { HStatusBar, HPriceTable, HSpreadMini, HPnlMini, HOppRow, hUsd, hSigned, hPct, hCap });
