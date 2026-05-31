/* global React, Icon */
// Brecha — nav rail (single-screen; items scroll to sections)

function NavRail({ active, onNav }) {
  const items = [
    { id: "top", icon: "layout-dashboard", label: "Resumen" },
    { id: "prices", icon: "radio", label: "Precios en vivo" },
    { id: "zscore", icon: "activity", label: "Z-score" },
    { id: "feed", icon: "arrow-left-right", label: "Oportunidades" },
    { id: "trades", icon: "list-checks", label: "Ejecuciones" },
  ];
  return (
    <nav className="bx-rail">
      <div className="bx-rail-top">
        <div className="bx-mark bx-mark-lg"><span className="b1" /><span className="b2" /></div>
      </div>
      <div className="bx-rail-items">
        {items.map((it) => (
          <button
            key={it.id}
            className={"bx-rail-item" + (active === it.id ? " active" : "")}
            onClick={() => onNav(it.id)}
            title={it.label}
          >
            <Icon name={it.icon} size={19} />
            <span className="bx-rail-tip">{it.label}</span>
          </button>
        ))}
      </div>
      <div className="bx-rail-bottom">
        <button className="bx-rail-item" title="Soporte"><Icon name="life-buoy" size={19} /><span className="bx-rail-tip">Soporte</span></button>
        <div className="bx-avatar">JM</div>
      </div>
    </nav>
  );
}
window.NavRail = NavRail;
