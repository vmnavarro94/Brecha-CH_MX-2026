/* global React, Icon, HStatusBar, HPriceTable, HSpreadMini, HPnlMini, HOppRow, hUsd, hSigned, hPct, hCap */

// ---------- scaffolding ----------
function Section({ num, title, sub, id, children }) {
  return (
    <section className="ho-section" id={id}>
      <div className="ho-sec-head"><span className="ho-sec-num">{num}</span><h2 className="ho-sec-title">{title}</h2></div>
      {sub && <p className="ho-sec-sub">{sub}</p>}
      {children}
    </section>
  );
}
function Card({ title, chip, chipCls, stageCls = "", anno, children }) {
  return (
    <div className="ho-card">
      <div className="ho-card-head">
        <span className="ho-card-title">{title}</span>
        {chip && <span className={"ho-chip " + (chipCls || "ho-chip-happy")}>{chip}</span>}
      </div>
      <div className={"ho-stage " + stageCls}>{children}</div>
      {anno && <div className="ho-anno">{anno.map((a, i) => <div className="ho-anno-row" key={i}><b>{a[0]}</b><span>{a[1]}</span></div>)}</div>}
    </div>
  );
}
const T = ({ children }) => <span className="ho-tok">{children}</span>;

// ---------- §6 decisions ----------
const DECISIONS = [
  { ref: "§6.1", q: "Viewport mobile", verdict: "decided", vlabel: "Desktop-first", a: <>Se entrega solo <b>desktop 1440px+</b> para v1. El jurado evalúa en laptop / proyector y el app shell fijo (rail 60px + status bar 78px + grid) no colapsa con dignidad bajo 768px. Responsive queda como fast-follow con un layout apilado, no bloquea la implementación.</> },
  { ref: "§6.2", q: "Pares en el z-score", verdict: "decided", vlabel: "3 unidireccionales", a: <>Se grafican los <b>3 pares unidireccionales</b> (binance·kraken, binance·bybit, kraken·bybit). Los 6 incluyen direcciones inversas que son espejo del mismo |spread| — redundantes. La leyenda permite togglear cada serie.</> },
  { ref: "§6.3", q: "Logos vs. texto", verdict: "decided", vlabel: "Texto + badge", a: <>Columna Exchange usa <b>texto capitalizado + badge de inicial</b> con color identificador por exchange. Evita las guías de uso de marca de Binance/Kraken/Bybit. Si product aprueba versiones white oficiales, se sustituye el badge por logo 20×20.</> },
  { ref: "§6.4", q: "Color de la score bar", verdict: "decided", vlabel: "Siempre --orange", a: <>Relleno <b>siempre <T>--orange</T></b>. El score es magnitud de prioridad de ejecución, no una señal buena/mala — no debe tomar prestado el verde/rojo, que están reservados a profit/loss.</> },
  { ref: "§6.5", q: "Persistencia del glow", verdict: "decided", vlabel: "Puntual 200ms", a: <>El <T>--glow-up</T> / <T>--glow-down</T> es un <b>flash puntual de 200ms</b> sobre la fila al ejecutarse, con fade-out. Nunca un loop persistente — el glow es puntuación, no decoración.</> },
  { ref: "§6.6", q: "Tweaks panel en producción", verdict: "confirm", vlabel: "Solo demo/dev", a: <>El panel de tweaks es <b>solo demo/dev</b>, oculto tras el edit-mode del host. El build del jurado se entrega sin él. <i>Confirmar con product si quieren un toggle oculto para el demo en vivo.</i></> },
];

// ---------- §1 constraints ----------
const CONSTRAINTS = [
  ["1", "Sin valores hardcoded", "Todo color, tamaño, spacing, radio y duración es var(--*) de colors_and_type.css. Nunca #1FCB8A — siempre --up."],
  ["2", "Sin emojis", "Ningún emoji en UI. Estados, errores, badges: texto + Lucide únicamente."],
  ["3", "Lucide, único set", "Stroke 1.75 · 16/18/20/24 · currentColor heredado, nunca color inline."],
  ["4", "Español primero", "Labels, verbos, headers, vacíos en español. Jargon en inglés: spread, order book, bps, P&L, z-score, slippage, circuit breaker."],
  ["5", "Números en mono", "font-mono + tabular-nums en todo valor numérico. Sin excepción. P&L hero: 38px / 500 / -0.03em."],
  ["6", "Color nunca es el único canal", "Cada estado por color lleva texto: ACTIVO/PAUSADO, +$/−$, Connected/Reconnecting, Live/Stale."],
  ["7", "Estados en tiempo real obligatorios", "Cada componente entrega carga, vacío, error y live. Un frame happy-path no basta."],
];

// ---------- §4 animations ----------
const ANIMS = [
  ["CB dot pulse (activo)", "Loop 1.5s / --mo", "Estado ACTIVO", "opacity 1 → .35 → 1 · ease-in-out"],
  ["CB pill blink (pausado)", "Loop 1.1s steps(1)", "Estado PAUSADO", "opacity 1 → .45 → 1 · sin ease"],
  ["Bid/ask flash sube", "200ms fade-out", "Cambio de precio", "fondo --up-wash aparece y desaparece"],
  ["Bid/ask flash baja", "200ms fade-out", "Cambio de precio", "fondo --down-wash aparece y desaparece"],
  ["Opportunity slide-in", "--dur-base (180ms)", "Nueva oportunidad", "translateY(-8px)→0 + opacity 0→1"],
  ["Número tick (P&L)", "--dur-fast (120ms)", "pnl_update", "flash breve / roll del número"],
  ["Nav rail tooltip", "130ms", "Hover en rail item", "translateX(-4px)→0 + opacity 0→1"],
  ["WS dot reconectando", "Loop 800ms", "wsConnected === false", "dot rojo parpadea + texto Reconnecting…"],
  ["Glow profit event", "200ms puntual", "trade_executed · Net>0", "--glow-up en la fila, fade-out"],
];

// ---------- token appendix ----------
const TOKENS = [
  ["--bg-base", "#07080A"], ["--bg-surface", "#0E1014"], ["--bg-surface-2", "#14171C"], ["--bg-elevated", "#1A1E25"], ["--bg-inset", "#0A0C0F"],
  ["--line-faint", "#15181D"], ["--line", "#22272F"], ["--line-strong", "#333A44"],
  ["--fg-1", "#F4F6FA"], ["--fg-2", "#AEB6C2"], ["--fg-3", "#717B89"], ["--fg-faint", "#4A535F"],
  ["--orange", "#F7931A"], ["--orange-bright", "#FFA831"], ["--orange-wash", "rgba(247,147,26,.12)"], ["--orange-line", "rgba(247,147,26,.35)"],
  ["--up", "#1FCB8A"], ["--up-wash", "rgba(31,203,138,.12)"], ["--down", "#FF4D57"], ["--down-wash", "rgba(255,77,87,.12)"], ["--info", "#3B9EFF"], ["--warn", "#FFC53D"],
];

// ---------- trade history sample ----------
const TRADE_ROWS = [
  { t: "14:21:08", buy: "kraken", sell: "bybit", vol: 0.01087296, bp: 72216.5, sp: 72251.4, fees: 1.72, net: 4.4 },
  { t: "14:20:54", buy: "binance", sell: "kraken", vol: 0.02215630, bp: 71988.2, sp: 72061.0, fees: 2.04, net: 8.96 },
  { t: "14:20:39", buy: "binance", sell: "bybit", vol: 0.00640200, bp: 72104.8, sp: 72119.3, fees: 0.94, net: -0.31 },
  { t: "14:20:21", buy: "bybit", sell: "kraken", vol: 0.01840900, bp: 71870.1, sp: 71944.6, fees: 1.88, net: 6.12 },
];

function HTradeTable({ page = "first" }) {
  const total = TRADE_ROWS.reduce((a, r) => a + r.net, 0);
  return (
    <div className="bx-panel" style={{ width: "100%" }}>
      <div className="bx-panel-head">
        <span className="bx-eyebrow"><Icon name="list-checks" size={13} /> Historial de trades</span>
        <div className="bx-pager">
          <span className="bx-pager-info">87 trades · pág. {page === "first" ? "1" : page === "mid" ? "3" : "5"}/5</span>
          <button className="bx-pager-btn" disabled={page === "first"}><Icon name="chevron-left" size={14} />Anterior</button>
          <button className="bx-pager-btn" disabled={page === "last"}>Siguiente<Icon name="chevron-right" size={14} /></button>
        </div>
      </div>
      <table className="bx-table">
        <thead><tr><th scope="col">Hora</th><th scope="col">Par</th><th scope="col" className="r">Volumen</th><th scope="col" className="r">Precio compra</th><th scope="col" className="r">Precio venta</th><th scope="col" className="r">Fees</th><th scope="col" className="r">Net P&amp;L</th></tr></thead>
        <tbody>
          {TRADE_ROWS.map((r, i) => (
            <tr key={i} style={i === 1 ? { background: "var(--bg-surface-2)" } : null}>
              <td className="num bx-muted">{r.t}</td>
              <td><span className="bx-route"><b className="bx-strong">{hCap(r.buy)}</b><Icon name="arrow-right" size={12} /><b className="bx-strong">{hCap(r.sell)}</b></span></td>
              <td className="r num">{r.vol.toFixed(8)} <span className="bx-muted">BTC</span></td>
              <td className="r num bx-muted">{hUsd(r.bp)}</td>
              <td className="r num bx-muted">{hUsd(r.sp)}</td>
              <td className="r num bx-muted">{hUsd(r.fees, 4)}</td>
              <td className={"r num " + (r.net > 0 ? "is-up" : "is-down")}>{hSigned(r.net)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot><tr className="bx-tfoot"><td colSpan="6">Total de la página ({TRADE_ROWS.length} trades)</td><td className={"r " + (total >= 0 ? "is-up" : "is-down")}>{hSigned(total)}</td></tr></tfoot>
      </table>
    </div>
  );
}

// ---------- nav rail sample ----------
function HNavRail() {
  const items = [["layout-dashboard", false], ["radio", true], ["activity", false], ["arrow-left-right", false], ["list-checks", false]];
  return (
    <nav className="bx-rail" style={{ height: 300, borderRadius: "var(--r-md)", border: "1px solid var(--line)" }}>
      <div className="bx-rail-top"><div className="bx-mark bx-mark-lg"><span className="b1" /><span className="b2" /></div></div>
      <div className="bx-rail-items">
        {items.map(([ic, active], i) => (
          <button key={i} className={"bx-rail-item" + (active ? " active" : "")} style={i === 2 ? { color: "var(--fg-1)", background: "var(--bg-surface-2)" } : null}><Icon name={ic} size={19} />{i === 2 && <span className="bx-rail-tip" style={{ opacity: 1, transform: "translateX(0)" }}>Hover</span>}</button>
        ))}
      </div>
    </nav>
  );
}

// build a smooth-ish z series
function zseries(seed, amp, drift) { const a = []; let v = seed; for (let i = 0; i < 40; i++) { v += (Math.sin(i / 5 + seed) * amp + drift) * 0.5; a.push(Math.max(-3.4, Math.min(3.4, v))); } return a; }

function Handoff() {
  return (
    <div className="ho-page">
      {/* COVER */}
      <header className="ho-cover">
        <div className="ho-cover-mark">
          <div className="bx-mark"><span className="b1" /><span className="b2" /></div>
          <div><div className="ho-cover-word">brecha</div><div className="ho-cover-tag">Design Handoff · v1</div></div>
        </div>
        <h1 className="ho-h1">Dashboard en tiempo real — handoff de estados</h1>
        <p className="ho-lead">Cada componente del cockpit, en todos sus estados documentados, anotado con los tokens exactos de <T>colors_and_type.css</T>. Fidelidad 100% al prototipo. Un componente no se implementa hasta tener todos sus estados aquí.</p>
        <div className="ho-sources">
          <span className="ho-source"><Icon name="file-code-2" size={14} />Verdad visual: <b>Brecha Dashboard.html</b></span>
          <span className="ho-source"><Icon name="palette" size={14} />Tokens: <b>app/colors_and_type.css</b></span>
          <span className="ho-source"><Icon name="shapes" size={14} />Íconos: <b>Lucide · stroke 1.75</b></span>
        </div>
      </header>

      {/* CONSTRAINTS */}
      <Section num="§1" title="Restricciones no negociables" id="constraints" sub="Las 7 reglas que validan el handoff. Cualquier frame que las contradiga se devuelve con observaciones antes de implementar.">
        <div className="ho-constraints">
          {CONSTRAINTS.map((c) => (
            <div className="ho-constraint" key={c[0]}><span className="ho-constraint-n">{c[0]}</span><div><div className="ho-constraint-t">{c[1]}</div><div className="ho-constraint-d">{c[2]}</div></div></div>
          ))}
        </div>
      </Section>

      {/* DECISIONS */}
      <Section num="§6" title="Ambigüedades resueltas" id="decisions" sub="Resoluciones de diseño propuestas. Verdes = decididas; ámbar = requiere confirmación de product antes de cerrar.">
        <div className="ho-decisions">
          {DECISIONS.map((d) => (
            <div className="ho-decision" key={d.ref}>
              <div className="ho-decision-q"><span className="ho-decision-ref">{d.ref}</span><span className="ho-decision-title">{d.q}</span></div>
              <span className={"ho-verdict " + d.verdict}><Icon name={d.verdict === "decided" ? "check-circle-2" : "help-circle"} size={13} />{d.vlabel}</span>
              <div className="ho-decision-a">{d.a}</div>
            </div>
          ))}
        </div>
      </Section>

      {/* STATUS BAR */}
      <Section num="§3.1" title="Status Bar" id="statusbar" sub="Barra siempre visible. Estados del circuit breaker, signo y color del P&L, y conexión WS.">
        <div className="ho-grid cols-1">
          <Card title="Sistema activo · P&L positivo" chip="happy path" anno={[["CB", <>pill <T>--up</T> + dot pulsante 1.5s</>], ["P&L", <><T>--up</T> · 38px / 500 / -0.03em mono</>], ["WS", <>dot <T>--up</T> + texto “Connected”</>]]}><HStatusBar cb="active" pnl={124.5} ws /></Card>
          <Card title="Sistema vigilando" chip="edge" chipCls="ho-chip-edge" anno={[["CB", <>pill <T>--warn</T>, sin parpadeo</>], ["Trigger", "pérdidas recientes · monitoreo activo"]]}><HStatusBar cb="watching" pnl={42.1} ws /></Card>
          <Card title="Sistema pausado · P&L negativo" chip="edge" chipCls="ho-chip-edge" anno={[["CB", <>pill <T>--down</T> parpadeante 1.1s steps(1)</>], ["P&L", <><T>--down</T> con signo −</>]]}><HStatusBar cb="paused" pnl={-3.2} ws /></Card>
          <div className="ho-grid cols-2">
            <Card title="Sesión nueva · P&L cero" chip="empty" chipCls="ho-chip-empty" anno={[["P&L", <><T>--fg-3</T> · “$0.00” sin signo coloreado</>]]}><HStatusBar cb="active" pnl={0} trades={0} exec={0} ws /></Card>
            <Card title="WebSocket desconectado" chip="error" chipCls="ho-chip-edge" anno={[["WS", <>dot <T>--down</T> parpadeante + “Reconnecting…”</>]]}><HStatusBar cb="active" pnl={124.5} ws={false} /></Card>
          </div>
        </div>
      </Section>

      {/* PRICE TABLE */}
      <Section num="§3.2" title="Price Table (BBO en vivo)" id="prices" sub="Tres filas, una por exchange. Badge de conexión por fila + flash de celda al cambiar el precio.">
        <div className="ho-grid cols-1">
          <Card title="Conexión: Live / Stale / Waiting" chip="3 estados de badge" anno={[["Live", <><T>--up</T> · update menor a 2s · dot pulsante</>], ["Stale", <><T>--warn</T> · sin update por 2s+</>], ["Waiting", <><T>--fg-3</T> · sin update desde carga · bid/ask “—”</>]]} stageCls="left">
            <HPriceTable rows={[{ ex: "binance", bid: 72018.09, ask: 72039.93, status: "live" }, { ex: "kraken", bid: 72048.78, ask: 72070.73, status: "stale" }, { ex: "bybit", bid: null, ask: null, status: "waiting" }]} />
          </Card>
          <Card title="Flash de celda al cambiar el precio" chip="refuerzo · 200ms" anno={[["Sube", <>fondo <T>--up-wash</T> 200ms fade · bid Binance</>], ["Baja", <>fondo <T>--down-wash</T> 200ms fade · ask Kraken</>], ["Regla", "el valor numérico siempre presente — el flash es refuerzo"]]} stageCls="left">
            <HPriceTable rows={[{ ex: "binance", bid: 72020.40, ask: 72041.10, status: "live", flashBid: "flash-up" }, { ex: "kraken", bid: 72048.78, ask: 72069.10, status: "live", flashAsk: "flash-down" }, { ex: "bybit", bid: 72027.63, ask: 72036.97, status: "live" }]} />
          </Card>
        </div>
      </Section>

      {/* SPREAD CHART */}
      <Section num="§3.3" title="Spread Chart (z-score en tiempo real)" id="spread" sub="Una línea por par. Eje Y z-score, eje X 60s. Líneas de referencia obligatorias en y=0, ±1σ (--warn punteada), ±2σ (--orange punteada).">
        <div className="ho-grid cols-2">
          <Card title="Calentando modelo (< 100 samples)" chip="bootstrap" chipCls="ho-chip-empty" anno={[["Badge", "“Calentando modelo: N/100 samples”"], ["Línea", <>tenue (opacity .4) hasta 100 samples</>]]}><HSpreadMini series={zseries(0.2, 0.18, 0)} color="var(--info)" warming samples={64} /></Card>
          <Card title="Activo normal (z entre ±2)" chip="happy path" anno={[["Línea", <>color por par (<T>--orange</T> / <T>--info</T> / <T>--up</T>)</>], ["Bandas", <>±1σ <T>--warn</T> · ±2σ <T>--orange-line</T></>]]}><HSpreadMini series={zseries(0.5, 0.5, 0)} color="var(--orange)" /></Card>
          <Card title="Anomalía (> 2σ)" chip="edge" chipCls="ho-chip-edge" anno={[["Zona", <>banda <T>--orange-wash</T> resaltada sobre +2σ</>], ["Lectura", "señal de oportunidad estadística"]]}><HSpreadMini series={zseries(1.6, 0.7, 0.04)} color="var(--up)" anomaly /></Card>
          <Card title="Marcadores de trade" chip="evento" anno={[["Ganador", <>punto <T>--up</T> en el momento de ejecución</>], ["Perdedor", <>punto <T>--down</T></>]]}><HSpreadMini series={zseries(1.2, 0.6, 0)} color="var(--orange)" marks={[{ i: 22, z: 2.3, win: true }, { i: 30, z: 2.7, win: true }, { i: 35, z: 1.1, win: false }]} /></Card>
        </div>
      </Section>

      {/* PNL CHART */}
      <Section num="§3.4" title="PnL Chart (P&L acumulado)" id="pnl" sub="Gráfico de área. Relleno verde/rojo según el último valor, línea base en y=0, tooltip al hover.">
        <div className="ho-grid cols-3">
          <Card title="Sin trades" chip="empty" chipCls="ho-chip-empty" anno={[["Texto", <>“Esperando primer trade…” <T>--fg-3</T></>]]}><HPnlMini data={null} /></Card>
          <Card title="P&L positivo" chip="happy path" anno={[["Relleno", <><T>--up-wash</T> · línea <T>--up</T></>], ["Base", <>y=0 en <T>--line</T></>]]}><HPnlMini data={[0, 6, 4, 12, 18, 16, 26, 34, 42]} /></Card>
          <Card title="P&L negativo" chip="edge" chipCls="ho-chip-edge" anno={[["Relleno", <><T>--down-wash</T> · línea <T>--down</T></>]]}><HPnlMini data={[0, -2, -5, -3, -9, -8, -14, -12, -18]} /></Card>
        </div>
        <div style={{ marginTop: "var(--sp-4)" }}>
          <Card title="Tooltip al hover" chip="interacción" anno={[["Popover", <>valor exacto + timestamp · fondo <T>--bg-elevated</T> borde <T>--line</T></>], ["Guía", <>línea vertical <T>--line-strong</T> punteada + punto en la serie</>]]}><div style={{ width: "100%", maxWidth: 560 }}><HPnlMini data={[0, 6, 4, 12, 18, 16, 26, 34, 42]} tooltip /></div></Card>
        </div>
      </Section>

      {/* OPPORTUNITY FEED */}
      <Section num="§3.5" title="Opportunity Feed" id="feed" sub="Últimas oportunidades, más recientes arriba. Cada fila: par, Net Profit %, z-score, score bar, status badge, timestamp.">
        <div className="ho-grid cols-2">
          <Card title="4 estados de badge + score bar" chip="happy path" anno={[["executed", <><T>--up</T> / --up-wash</>], ["skipped", <><T>--fg-3</T> / --bg-surface-2</>], ["expired", <><T>--orange</T> / --orange-wash</>], ["detected", <><T>--info</T> / --info-wash</>], ["score bar", <>0→1 · relleno <T>--orange</T></>]]} stageCls="left">
            <div className="ho-feed-frame">
              <HOppRow op={{ buy: "kraken", sell: "bybit", pct: 0.00336, z: -3.6, score: 0.94, status: "executed", t: "14:21:20" }} />
              <HOppRow op={{ buy: "binance", sell: "bybit", pct: 0.00099, z: -0.22, score: 0.36, status: "skipped", t: "14:21:18" }} />
              <HOppRow op={{ buy: "binance", sell: "kraken", pct: 0.0031, z: 3.36, score: 0.87, status: "expired", t: "14:21:15" }} />
              <HOppRow op={{ buy: "binance", sell: "bybit", pct: 0.00185, z: 1.48, score: 0.7, status: "detected", t: "14:21:12" }} />
            </div>
          </Card>
          <div style={{ display: "flex", flexDirection: "column", gap: "var(--sp-4)" }}>
            <Card title="Z-score anómalo (> ±2)" chip="resalte" anno={[["Badge z", <>resaltado <T>--orange</T> / --orange-wash cuando |z| > 2; normal <T>--info</T></>]]} stageCls="left">
              <div className="ho-feed-frame"><HOppRow op={{ buy: "kraken", sell: "bybit", pct: 0.0042, z: 2.57, score: 0.91, status: "executed", t: "14:21:09" }} /></div>
            </Card>
            <Card title="Feed vacío" chip="empty" chipCls="ho-chip-empty" anno={[["Texto", "“El motor está escuchando. Sin oportunidades por ahora.”"], ["Animación", <>row nueva: slide-in <T>--dur-base</T></>]]} stageCls="left">
              <div className="ho-feed-frame"><div style={{ padding: "28px 16px", textAlign: "center", color: "var(--fg-3)", fontFamily: "var(--font-mono)", fontSize: 12 }}>El motor está escuchando.<br />Sin oportunidades por ahora.</div></div>
            </Card>
          </div>
        </div>
      </Section>

      {/* TRADE HISTORY */}
      <Section num="§3.6" title="Trade History" id="trades" sub="Tabla paginada (20/página). Net P&L con signo + color. Fila de totales por página. Estados de paginación.">
        <div className="ho-grid cols-1">
          <Card title="Con data · ganancia + pérdida + totales + hover" chip="happy path" anno={[["Ganancia", <>celda <T>--up</T> · +$</>], ["Pérdida", <>celda <T>--down</T> · −$</>], ["Hover", <>fila a <T>--bg-surface-2</T> (fila 2 mostrada)</>], ["Totales", <>última fila · suma de la página · <T>--bg-inset</T></>]]} stageCls="left"><HTradeTable page="mid" /></Card>
          <div className="ho-grid cols-3">
            <Card title="Paginación · primera" chip="empty" chipCls="ho-chip-empty" anno={[["Anterior", "deshabilitado"]]} stageCls="left"><div style={{ width: "100%" }}><PagerBar page="first" /></div></Card>
            <Card title="Paginación · intermedia" chip="happy path" anno={[["Ambos", "activos"]]} stageCls="left"><div style={{ width: "100%" }}><PagerBar page="mid" /></div></Card>
            <Card title="Paginación · última" chip="empty" chipCls="ho-chip-empty" anno={[["Siguiente", "deshabilitado"]]} stageCls="left"><div style={{ width: "100%" }}><PagerBar page="last" /></div></Card>
          </div>
        </div>
      </Section>

      {/* NAV RAIL */}
      <Section num="§3.7" title="Nav Rail" id="rail" sub="Rail izquierdo 60px. Íconos sin labels (tooltip en hover).">
        <div className="ho-grid cols-1">
          <Card title="Item activo · hover · inactivo" chip="3 estados" anno={[["Activo", <>fondo <T>--orange-wash</T> · ícono <T>--orange</T> · indicador 3px borde izq.</>], ["Hover", <>fondo <T>--bg-surface-2</T> · ícono <T>--fg-1</T> · tooltip slide-in (item 3)</>], ["Inactivo", <>transparente · ícono <T>--fg-3</T></>]]}>
            <div className="ho-rail-frame"><HNavRail /></div>
          </Card>
        </div>
      </Section>

      {/* ANIMATIONS */}
      <Section num="§4" title="Animaciones" id="anims" sub="Grabaciones de 2–3s, fondo negro, sin UI externo. Tabla de especificación + demos vivos abajo.">
        <div className="ho-table-wrap">
          <table className="bx-table">
            <thead><tr><th scope="col">Animación</th><th scope="col">Duración</th><th scope="col">Trigger</th><th scope="col">Notas</th></tr></thead>
            <tbody>{ANIMS.map((a, i) => <tr key={i}><td className="bx-strong">{a[0]}</td><td className="num">{a[1]}</td><td className="bx-muted">{a[2]}</td><td className="bx-muted">{a[3]}</td></tr>)}</tbody>
          </table>
        </div>
        <div className="ho-grid cols-4" style={{ marginTop: "var(--sp-4)" }}>
          <Card title="CB pulse · activo" chip="loop"><span className="bx-cb-pill bx-cb-active"><span className="bx-cb-dot" />ACTIVO</span></Card>
          <Card title="CB blink · pausado" chip="loop"><span className="bx-cb-pill bx-cb-paused"><span className="bx-cb-dot" />PAUSADO</span></Card>
          <Card title="WS reconectando" chip="loop"><div className="bx-ws down"><span className="bx-ws-dot" /><span className="bx-ws-label">Reconnecting…</span></div></Card>
          <Card title="Status: Live" chip="loop"><span className="bx-px-status bx-px-live"><span className="d" />Live</span></Card>
        </div>
      </Section>

      {/* ASSETS */}
      <Section num="§5" title="Assets" id="assets" sub="Inventario. El mark se implementa en CSS (dos barras offset); confirmar si existe SVG oficial que lo reemplace.">
        <div className="ho-grid cols-2">
          <Card title="Brecha mark" chip="CSS · confirmar SVG" chipCls="ho-chip-edge" anno={[["Uso", "nav rail header · status bar brand"], ["Color", <>siempre <T>--orange</T> · nunca recolorear</>]]}>
            <div className="ho-asset"><div className="ho-asset-preview"><div className="bx-mark bx-mark-lg"><span className="b1" /><span className="b2" /></div></div><div style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-3)" }}>Dos barras offset =<br />dos niveles de precio<br />desalineados (la brecha)</div></div>
          </Card>
          <Card title="Exchange identity" chip="texto + badge" anno={[["Decisión", <>§6.3 · badge de inicial con color por exchange</>], ["Tamaño", "26×26 · radio --r-sm"]]}>
            <div style={{ display: "flex", gap: 14 }}>
              {Object.entries({ binance: "#F3BA2F", kraken: "#7B68EE", bybit: "#F7A600" }).map(([k, c]) => (
                <div key={k} style={{ display: "flex", alignItems: "center", gap: 8 }}><span className="bx-px-badge" style={{ background: c + "22", color: c }}>{hCap(k)[0]}</span><span className="bx-px-name">{hCap(k)}</span></div>
              ))}
            </div>
          </Card>
        </div>
      </Section>

      {/* TOKENS */}
      <Section num="§A" title="Tokens más usados" id="tokens" sub="Referencia rápida. La lista completa (80 tokens) vive en colors_and_type.css — esta es la fuente de verdad.">
        <div className="ho-tokens">
          {TOKENS.map(([name, val]) => (
            <div className="ho-token" key={name}><span className="ho-token-sw" style={{ background: `var(${name})` }} /><div className="ho-token-meta"><span className="ho-token-name">{name}</span><span className="ho-token-val">{val}</span></div></div>
          ))}
        </div>
      </Section>

      <footer style={{ marginTop: "var(--sp-16)", paddingTop: "var(--sp-6)", borderTop: "1px solid var(--line)", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-faint)", letterSpacing: ".04em" }}>
        BRECHA · DESIGN HANDOFF v1 · captura el spread
      </footer>
    </div>
  );
}

function PagerBar({ page }) {
  return (
    <div className="bx-panel-head" style={{ borderBottom: 0, justifyContent: "flex-end", borderRadius: "var(--r-md)", border: "1px solid var(--line)", background: "var(--bg-surface)" }}>
      <div className="bx-pager">
        <span className="bx-pager-info">pág. {page === "first" ? "1" : page === "mid" ? "3" : "5"}/5</span>
        <button className="bx-pager-btn" disabled={page === "first"}><Icon name="chevron-left" size={14} />Anterior</button>
        <button className="bx-pager-btn" disabled={page === "last"}>Siguiente<Icon name="chevron-right" size={14} /></button>
      </div>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<Handoff />);
