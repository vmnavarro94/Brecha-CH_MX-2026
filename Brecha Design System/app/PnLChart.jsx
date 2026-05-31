/* global React, Icon */
// Brecha — PnLChart: cumulative P&L area chart over the session
const { useStore, fmtSigned, fmtHM } = window.BX;

function PnLChart() {
  const histRaw = useStore((s) => s.pnlHistory);
  const hist = React.useMemo(() => histRaw.slice().sort((a, b) => a.time - b.time), [histRaw]);
  const [dims, setDims] = React.useState({ w: 700, h: 200 });
  const [hover, setHover] = React.useState(null);
  const bodyRef = React.useRef(null);

  React.useLayoutEffect(() => {
    const el = bodyRef.current; if (!el) return;
    const ro = new ResizeObserver(() => setDims({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el); setDims({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const last = hist.length ? hist[hist.length - 1].value : 0;
  const positive = last >= 0;
  const color = positive ? "var(--up)" : "var(--down)";

  const PADL = 40, PADR = 12, PADT = 12, PADB = 20;
  const W = Math.max(320, dims.w), H = 200;
  const plotW = W - PADL - PADR, plotH = H - PADT - PADB;

  let body = null, axisY = [], tip = null;
  if (hist.length >= 2) {
    const t0 = hist[0].time, t1 = hist[hist.length - 1].time;
    const vals = hist.map((p) => p.value);
    const vmax = Math.max(0, ...vals), vmin = Math.min(0, ...vals);
    const pad = (vmax - vmin) * 0.12 || 1;
    const lo = vmin - pad, hi = vmax + pad;
    const x = (t) => PADL + (t1 === t0 ? 0.5 : (t - t0) / (t1 - t0)) * plotW;
    const y = (v) => PADT + (1 - (v - lo) / (hi - lo)) * plotH;
    const pts = hist.map((p) => [x(p.time), y(p.value)]);
    const line = pts.map((p, i) => (i ? "L" : "M") + p[0].toFixed(1) + " " + p[1].toFixed(1)).join(" ");
    const y0 = Math.max(PADT, Math.min(PADT + plotH, y(0)));
    const area = line + ` L ${pts[pts.length - 1][0].toFixed(1)} ${y0} L ${pts[0][0].toFixed(1)} ${y0} Z`;

    // y ticks
    const ticks = 3;
    for (let i = 0; i <= ticks; i++) { const v = lo + (hi - lo) * (i / ticks); axisY.push({ v, y: y(v) }); }

    body = (
      <svg width={W} height={H} style={{ display: "block" }}
        onMouseMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          const mx = e.clientX - rect.left;
          let best = 0, bd = Infinity;
          pts.forEach((p, i) => { const d = Math.abs(p[0] - mx); if (d < bd) { bd = d; best = i; } });
          setHover({ i: best, px: pts[best][0], py: pts[best][1] });
        }}
        onMouseLeave={() => setHover(null)}>
        <defs>
          <linearGradient id="pnlg" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.30" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        {axisY.map((t, i) => (
          <g key={i}>
            <line x1={PADL} y1={t.y} x2={W - PADR} y2={t.y} stroke="var(--line-faint)" strokeWidth="1" />
            <text className="bx-zaxis" x={PADL - 6} y={t.y + 3} textAnchor="end">{(t.v >= 0 ? "$" : "−$") + Math.abs(Math.round(t.v))}</text>
          </g>
        ))}
        <line x1={PADL} y1={y0} x2={W - PADR} y2={y0} stroke="var(--line)" strokeWidth="1" />
        <path className={positive ? "pnl-area-pos" : "pnl-area-neg"} d={area} fill="url(#pnlg)" />
        <path d={line} fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round" />
        {hover && (
          <g>
            <line x1={hover.px} y1={PADT} x2={hover.px} y2={PADT + plotH} stroke="var(--line-strong)" strokeWidth="1" strokeDasharray="2 3" />
            <circle cx={hover.px} cy={hover.py} r="4" fill={color} stroke="var(--bg-surface)" strokeWidth="1.5" />
          </g>
        )}
      </svg>
    );
    if (hover) {
      const p = hist[hover.i];
      tip = (
        <div className="bx-pnl-tip" style={{ left: hover.px, top: hover.py }}>
          <div className={"v " + (p.value >= 0 ? "is-up" : "is-down")}>{fmtSigned(p.value)}</div>
          <div className="t">{new Date(p.time).toLocaleTimeString("es-MX", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })}</div>
        </div>
      );
    }
  }

  return (
    <div className="bx-panel" id="bx-pnl">
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="line-chart" size={13} /> P&amp;L acumulado</span>
        <span className="bx-head-meta" style={{ color }}>{hist.length ? fmtSigned(last) : "—"}</span>
      </div>
      <div className="bx-pnlchart-body" ref={bodyRef}>
        {hist.length < 2 ? (
          <div className="bx-pnl-empty"><Icon name="hourglass" size={20} />Esperando primer trade…</div>
        ) : body}
        {tip}
      </div>
    </div>
  );
}
window.PnLChart = PnLChart;
