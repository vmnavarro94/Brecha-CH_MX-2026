/* global React, Icon */
// Brecha — SpreadChart: real-time z-score per exchange pair
const { useStore, FEATURED_PAIRS, PAIR_COLOR } = window.BX;

const PAIR_LABEL = {
  "binance-kraken": "Binance · Kraken",
  "binance-bybit": "Binance · Bybit",
  "kraken-bybit": "Kraken · Bybit",
};
const Z_MIN = -3.5, Z_MAX = 3.5, WINDOW = 60000;

function SpreadChart() {
  const zSeries = useStore((s) => s.zSeries);
  const spreads = useStore((s) => s.spreads);
  const tradeMarks = useStore((s) => s.tradeMarks);
  const [vis, setVis] = React.useState({ "binance-kraken": true, "binance-bybit": true, "kraken-bybit": true });
  const [dims, setDims] = React.useState({ w: 760, h: 270 });
  const bodyRef = React.useRef(null);

  React.useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setDims({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el);
    setDims({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const PADL = 30, PADR = 14, PADT = 14, PADB = 18;
  const W = Math.max(320, dims.w), H = 250;
  const plotW = W - PADL - PADR, plotH = H - PADT - PADB;
  const now = Date.now();
  const x = (t) => PADL + Math.max(0, Math.min(1, (t - (now - WINDOW)) / WINDOW)) * plotW;
  const y = (z) => PADT + (1 - (z - Z_MIN) / (Z_MAX - Z_MIN)) * plotH;

  const statFor = (p) => spreads.find((s) => s.Pair === p);
  const warming = FEATURED_PAIRS.filter((p) => vis[p] && statFor(p) && statFor(p).Samples < 100);
  const refLines = [
    { z: 2, c: "var(--orange-line)", dash: "3 4", lbl: "+2σ" },
    { z: 1, c: "rgba(255,197,61,0.28)", dash: "2 5", lbl: "+1σ" },
    { z: 0, c: "var(--line)", dash: "", lbl: "0" },
    { z: -1, c: "rgba(255,197,61,0.28)", dash: "2 5", lbl: "−1σ" },
    { z: -2, c: "var(--orange-line)", dash: "3 4", lbl: "−2σ" },
  ];

  function pathFor(pair) {
    const pts = (zSeries[pair] || []);
    if (pts.length < 2) return "";
    return pts.map((p, i) => (i ? "L" : "M") + x(p.t).toFixed(1) + " " + y(p.z).toFixed(1)).join(" ");
  }

  return (
    <div className="bx-panel bx-spread" id="bx-zscore">
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="activity" size={13} /> Z-score del spread · ventana 60 s</span>
        <span className="bx-head-meta">(spread − μ) / σ · por par</span>
      </div>

      <div className="bx-spread-body" ref={bodyRef}>
        {warming.length > 0 && (
          <div className="bx-warming">
            <Icon name="loader" size={12} />
            Calentando modelo: {statFor(warming[0]).Samples}/100 samples
            <span className="bar"><span style={{ width: Math.min(100, statFor(warming[0]).Samples) + "%" }} /></span>
          </div>
        )}
        <svg width={W} height={H} style={{ display: "block" }}>
          {/* reference lines */}
          {refLines.map((r) => (
            <g key={r.z}>
              <line x1={PADL} y1={y(r.z)} x2={W - PADR} y2={y(r.z)} stroke={r.c} strokeWidth="1" strokeDasharray={r.dash} />
              <text className="bx-zaxis" x={PADL - 6} y={y(r.z) + 3} textAnchor="end">{r.lbl}</text>
            </g>
          ))}
          {/* anomaly bands tint above ±2 */}
          <rect x={PADL} y={y(Z_MAX)} width={plotW} height={y(2) - y(Z_MAX)} fill="var(--orange-wash)" opacity="0.4" />
          <rect x={PADL} y={y(-2)} width={plotW} height={y(Z_MIN) - y(-2)} fill="var(--orange-wash)" opacity="0.4" />

          {/* series */}
          {FEATURED_PAIRS.map((pair) => vis[pair] && (
            <path key={pair} d={pathFor(pair)} fill="none" stroke={PAIR_COLOR[pair]} strokeWidth="1.6"
              strokeLinejoin="round" strokeLinecap="round" opacity={statFor(pair) && statFor(pair).Samples < 100 ? 0.4 : 1} />
          ))}

          {/* current-value end dots */}
          {FEATURED_PAIRS.map((pair) => {
            const arr = zSeries[pair]; if (!vis[pair] || !arr || !arr.length) return null;
            const last = arr[arr.length - 1];
            return <circle key={pair} cx={x(last.t)} cy={y(last.z)} r="3" fill={PAIR_COLOR[pair]} />;
          })}

          {/* trade markers */}
          {FEATURED_PAIRS.map((pair) => vis[pair] && (tradeMarks[pair] || []).filter((m) => now - m.t < WINDOW).map((m, i) => (
            <g key={pair + i} className="bx-trade-marker">
              <circle cx={x(m.t)} cy={y(m.z)} r="6" fill="none" stroke={m.profit >= 0 ? "var(--up)" : "var(--down)"} strokeWidth="1.4" opacity="0.9" />
              <circle cx={x(m.t)} cy={y(m.z)} r="2.4" fill={m.profit >= 0 ? "var(--up)" : "var(--down)"} />
            </g>
          )))}
        </svg>
      </div>

      <div className="bx-zlegend">
        {FEATURED_PAIRS.map((pair) => {
          const stat = statFor(pair);
          const arr = zSeries[pair];
          const z = arr && arr.length ? arr[arr.length - 1].z : 0;
          return (
            <div key={pair} className={"bx-zleg-item" + (vis[pair] ? " on" : "")} onClick={() => setVis((v) => ({ ...v, [pair]: !v[pair] }))}>
              <span className="bx-zleg-swatch" style={{ background: PAIR_COLOR[pair] }} />
              <span className="bx-zleg-label">{PAIR_LABEL[pair]}</span>
              <span className="bx-zleg-z" style={{ color: Math.abs(z) > 2 ? "var(--orange)" : undefined }}>z {z >= 0 ? "+" : "−"}{Math.abs(z).toFixed(2)}</span>
              {stat && stat.Samples < 100 && <span className="bx-zleg-z" style={{ color: "var(--warn)" }}>· {stat.Samples}/100</span>}
            </div>
          );
        })}
        <div className="bx-zleg-item on" style={{ marginLeft: "auto", cursor: "default", gap: 6 }}>
          <Icon name="dot" size={14} className="is-up" /><span className="bx-zleg-label" style={{ color: "var(--fg-3)" }}>trade ejecutado</span>
        </div>
      </div>
    </div>
  );
}
window.SpreadChart = SpreadChart;
