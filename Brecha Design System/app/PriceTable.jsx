/* global React, Icon */
// Brecha — PriceTable: live BBO for the 3 exchanges
const { useStore } = window.BX;

const EX_META = {
  binance: { label: "Binance", color: "#F3BA2F" },
  kraken:  { label: "Kraken",  color: "#7B68EE" },
  bybit:   { label: "Bybit",   color: "#F7A600" },
};
const px2 = (n) => n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

function PriceRow({ ex, p, now }) {
  const prev = React.useRef({ bid: null, ask: null });
  const [flash, setFlash] = React.useState({ bid: "", ask: "" });
  const meta = EX_META[ex];

  React.useEffect(() => {
    if (!p) return;
    const next = { bid: "", ask: "" };
    if (prev.current.bid != null && p.bid !== prev.current.bid) next.bid = p.bid > prev.current.bid ? "flash-up" : "flash-down";
    if (prev.current.ask != null && p.ask !== prev.current.ask) next.ask = p.ask > prev.current.ask ? "flash-up" : "flash-down";
    prev.current = { bid: p.bid, ask: p.ask };
    if (next.bid || next.ask) {
      setFlash(next);
      const t = setTimeout(() => setFlash({ bid: "", ask: "" }), 300);
      return () => clearTimeout(t);
    }
  }, [p && p.bid, p && p.ask]);

  let statusCls = "bx-px-wait", statusLabel = "Waiting…";
  if (p) {
    const age = now - p.receivedAt;
    if (age < 2000) { statusCls = "bx-px-live"; statusLabel = "Live"; }
    else { statusCls = "bx-px-stale"; statusLabel = "Stale"; }
  }
  const spread = p ? ((p.ask - p.bid) / p.ask * 100) : null;

  return (
    <tr>
      <td>
        <div className="bx-px-ex">
          <span className="bx-px-badge" style={{ background: meta.color + "22", color: meta.color }}>{meta.label[0]}</span>
          <span className="bx-px-name">{meta.label}</span>
        </div>
      </td>
      <td><span className={"bx-px-cell bx-px-bid " + flash.bid}>{p ? px2(p.bid) : "—"}</span></td>
      <td><span className={"bx-px-cell bx-px-ask " + flash.ask}>{p ? px2(p.ask) : "—"}</span></td>
      <td><span className="bx-px-spread num">{spread != null ? spread.toFixed(4) + "%" : "—"}</span></td>
      <td><span className={"bx-px-status " + statusCls}><span className="d" />{statusLabel}</span></td>
    </tr>
  );
}

function PriceTable() {
  const prices = useStore((s) => s.prices);
  const [now, setNow] = React.useState(Date.now());
  React.useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(iv);
  }, []);
  return (
    <div className="bx-panel bx-prices" id="bx-prices">
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="radio" size={13} /> Precios en vivo · BBO</span>
        <span className="bx-head-meta">BTC/USDT · 250 ms</span>
      </div>
      <table className="bx-ptable">
        <thead>
          <tr>
            <th scope="col">Exchange</th>
            <th scope="col">Bid</th>
            <th scope="col">Ask</th>
            <th scope="col">Spread</th>
            <th scope="col">Estado</th>
          </tr>
        </thead>
        <tbody>
          {window.BX.EXCHANGES.map((ex) => (
            <PriceRow key={ex} ex={ex} p={prices[ex]} now={now} />
          ))}
        </tbody>
      </table>
    </div>
  );
}
window.PriceTable = React.memo(PriceTable);
