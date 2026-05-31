/* global React, Icon */
// Brecha — TradeHistory: paginated ledger of executed trades
const { useStore, fmtSigned, fmtUsd, fmtTime, cap } = window.BX;
const PER_PAGE = 20;

function TradeHistory() {
  const trades = useStore((s) => s.trades);
  const lastId = useStore((s) => s.lastTradeId);
  const [page, setPage] = React.useState(0);

  const pages = Math.max(1, Math.ceil(trades.length / PER_PAGE));
  const clamped = Math.min(page, pages - 1);
  React.useEffect(() => { if (page !== clamped) setPage(clamped); }, [clamped, page]);

  const start = clamped * PER_PAGE;
  const slice = trades.slice(start, start + PER_PAGE);
  const pageTotal = slice.reduce((a, t) => a + t.NetProfit, 0);

  return (
    <div className="bx-panel" id="bx-trades">
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="list-checks" size={13} /> Historial de trades</span>
        <div className="bx-pager">
          <span className="bx-pager-info">{trades.length} trades · pág. {clamped + 1}/{pages}</span>
          <button className="bx-pager-btn" disabled={clamped === 0} onClick={() => setPage(clamped - 1)}><Icon name="chevron-left" size={14} />Anterior</button>
          <button className="bx-pager-btn" disabled={clamped >= pages - 1} onClick={() => setPage(clamped + 1)}>Siguiente<Icon name="chevron-right" size={14} /></button>
        </div>
      </div>
      <table className="bx-table">
        <thead>
          <tr>
            <th scope="col">Hora</th>
            <th scope="col">Par</th>
            <th scope="col" className="r">Volumen</th>
            <th scope="col" className="r">Precio compra</th>
            <th scope="col" className="r">Precio venta</th>
            <th scope="col" className="r">Fees</th>
            <th scope="col" className="r">Net P&amp;L</th>
          </tr>
        </thead>
        <tbody>
          {slice.map((t) => (
            <tr key={t.ID} className={t.ID === lastId && clamped === 0 ? "fresh" : ""}>
              <td className="num bx-muted">{fmtTime(t.ExecutedAt)}</td>
              <td>
                <span className="bx-route"><b className="bx-strong">{cap(t.BuyExchange)}</b><Icon name="arrow-right" size={12} /><b className="bx-strong">{cap(t.SellExchange)}</b></span>
              </td>
              <td className="r num">{t.Volume.toFixed(8)} <span className="bx-muted">BTC</span></td>
              <td className="r num bx-muted">{fmtUsd(t.BuyPrice)}</td>
              <td className="r num bx-muted">{fmtUsd(t.SellPrice)}</td>
              <td className="r num bx-muted">{fmtUsd(t.Fees, 4)}</td>
              <td className={"r num " + (t.NetProfit > 0 ? "is-up" : t.NetProfit < 0 ? "is-down" : "is-flat")}>{fmtSigned(t.NetProfit)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="bx-tfoot">
            <td colSpan="6">Total de la página ({slice.length} trades)</td>
            <td className={"r " + (pageTotal >= 0 ? "is-up" : "is-down")}>{fmtSigned(pageTotal)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
}
window.TradeHistory = TradeHistory;
