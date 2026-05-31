/* global React, Icon */
// Brecha — StatusBar: always-visible system header
const { useStore, fmtSigned, EXCHANGES } = window.BX;

const CB_MAP = {
  active:   { cls: "bx-cb-active",   label: "ACTIVO",   sub: "Operando normalmente" },
  watching: { cls: "bx-cb-watching", label: "VIGILANDO", sub: "Pérdidas recientes · monitoreo" },
  paused:   { cls: "bx-cb-paused",   label: "PAUSADO",  sub: "Circuit breaker disparado" },
};

function StatusBar() {
  const cb = useStore((s) => s.circuitBreakerState);
  const pnlVal = useStore((s) => s.pnl.total_pnl);
  const winRate = useStore((s) => s.pnl.win_rate);
  const tradeCount = useStore((s) => s.trades.length);
  const wsConnected = useStore((s) => s.wsConnected);
  const execCount = useStore((s) => s.opportunities.filter((o) => o.Status === "executed").length);

  const cbInfo = CB_MAP[cb] || CB_MAP.active;
  const cls = pnlVal > 0 ? "is-up" : pnlVal < 0 ? "is-down" : "is-flat";
  const liveExchanges = EXCHANGES.length;

  return (
    <header className="bx-status" id="bx-top">
      <div className="bx-status-brand">
        <div className="bx-mark"><span className="b1" /><span className="b2" /></div>
        <div>
          <div className="bx-word">brecha</div>
          <div className="bx-word-sub">Arbitrage Engine</div>
        </div>
      </div>

      <div className="bx-cb">
        <span className="bx-cb-k">Circuit Breaker</span>
        <span className={"bx-cb-pill " + cbInfo.cls}>
          <span className="bx-cb-dot" />{cbInfo.label}
        </span>
      </div>

      <div className="bx-pnl-hero">
        <span className="bx-pnl-hero-k">P&amp;L acumulado · sesión</span>
        <span className={"bx-pnl-hero-v " + cls}>{fmtSigned(pnlVal)}</span>
        <span className="bx-pnl-hero-sub">
          <Icon name="trending-up" size={13} className={cls} />
          {tradeCount} trades · {execCount} ejecutadas en vivo
        </span>
      </div>

      <div className="bx-stat-grp">
        <div className="bx-stat">
          <span className="bx-stat-k">Tasa de acierto</span>
          <span className="bx-stat-v">{(winRate * 100).toFixed(1)}%</span>
        </div>
        <div className="bx-stat">
          <span className="bx-stat-k">Exchanges</span>
          <span className="bx-stat-v">{liveExchanges}<small> / 3</small></span>
        </div>
        <div className="bx-stat">
          <span className="bx-stat-k">Trades</span>
          <span className="bx-stat-v">{tradeCount}</span>
        </div>
      </div>

      <div className={"bx-ws " + (wsConnected ? "up" : "down")}>
        <span className="bx-ws-dot" />
        <span className="bx-ws-label">{wsConnected ? "Connected" : "Reconnecting…"}</span>
      </div>
    </header>
  );
}
window.StatusBar = StatusBar;
