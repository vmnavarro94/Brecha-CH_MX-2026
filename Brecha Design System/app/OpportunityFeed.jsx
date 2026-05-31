/* global React, Icon */
// Brecha — OpportunityFeed: live stream of detected opportunities
const { useStore, fmtPct, fmtTime, cap } = window.BX;

const ST_MAP = {
  executed: { cls: "bx-st-executed", icon: "check", label: "Ejecutada" },
  skipped:  { cls: "bx-st-skipped",  icon: "minus", label: "Descartada" },
  expired:  { cls: "bx-st-expired",  icon: "clock", label: "Expirada" },
  detected: { cls: "bx-st-detected", icon: "search", label: "Detectada" },
};

function OppRow({ op, fresh }) {
  const st = ST_MAP[op.Status] || ST_MAP.detected;
  const hot = Math.abs(op.ZScore) > 2;
  const pctCls = op.NetProfitPct >= 0 ? "is-up" : "is-down";
  return (
    <div className={"bx-opp" + (fresh ? " enter" : "")}>
      <div className="bx-opp-top">
        <span className="bx-opp-route">
          <b>{cap(op.BuyExchange)}</b><Icon name="arrow-right" size={13} /><b>{cap(op.SellExchange)}</b>
        </span>
        <span className={"bx-stbadge " + st.cls}><Icon name={st.icon} size={11} />{st.label}</span>
        <span className="bx-opp-time">{fmtTime(op.DetectedAt)}</span>
      </div>
      <div className="bx-opp-bottom">
        <span className={"bx-opp-pct " + pctCls}>{fmtPct(op.NetProfitPct)}</span>
        <span className="bx-opp-zwrap">
          z<span className={"bx-opp-zbadge" + (hot ? " hot" : "")}>{op.ZScore >= 0 ? "+" : "−"}{Math.abs(op.ZScore).toFixed(2)}</span>
        </span>
        <span className="bx-opp-score">
          <span className="bx-opp-scorebar"><span style={{ width: (op.Score * 100).toFixed(0) + "%" }} /></span>
          <span className="bx-opp-scoreval">{op.Score.toFixed(2)}</span>
        </span>
      </div>
    </div>
  );
}

function OpportunityFeed() {
  const opps = useStore((s) => s.opportunities);
  const lastId = useStore((s) => s.lastOppId);
  const visible = opps.slice(0, 50);
  return (
    <div className="bx-panel bx-feed" id="bx-feed">
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="arrow-left-right" size={13} /> Oportunidades en vivo</span>
        <span className="bx-head-meta">{opps.length} en buffer · top 50</span>
      </div>
      <div className="bx-feed-rows">
        {visible.length === 0 && (
          <div style={{ padding: "28px 16px", textAlign: "center", color: "var(--fg-3)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
            Sin oportunidades por ahora. El bot sigue escuchando.
          </div>
        )}
        {visible.map((op) => (
          <OppRow key={op.ID} op={op} fresh={op.ID === lastId} />
        ))}
      </div>
    </div>
  );
}
window.OpportunityFeed = OpportunityFeed;
