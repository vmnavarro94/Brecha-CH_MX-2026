/* global React, NavRail, StatusBar, PriceTable, SpreadChart, PnLChart, OpportunityFeed, TradeHistory,
   useTweaks, TweaksPanel, TweakSection, TweakSlider, TweakRadio, TweakColor */
const { useState, useEffect, useRef, useCallback } = React;
const BX = window.BX;

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "accent": "#F7931A",
  "motion": 6,
  "feed": 4,
  "density": "regular",
  "circuit": "active"
}/*EDITMODE-END*/;

const ACCENTS = {
  "#F7931A": { bright: "#FFA831", dim: "#B96A0E" },
  "#3B9EFF": { bright: "#62B2FF", dim: "#2A6FBF" },
  "#7A5AE0": { bright: "#9B82F0", dim: "#5B40B0" },
  "#19C0C8": { bright: "#46DCE3", dim: "#0E8A90" },
};
function hexA(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}

function App() {
  const [t, setTweak] = useTweaks(TWEAK_DEFAULTS);
  const [active, setActive] = useState("top");
  const contentRef = useRef(null);
  const backendRef = useRef(null);
  const scrollIvRef = useRef(null);

  // cfg the simulation reads live
  const cfgRef = useRef({ circuitBreaker: t.circuit, feedRate: t.feed / 4 });
  cfgRef.current = { circuitBreaker: t.circuit, feedRate: Math.max(0.25, t.feed / 4) };

  // ----- boot: seed + start mock backend -----
  useEffect(() => {
    BX.seedInitial();
    const be = BX.MockBackend(BX.dispatchEvent, () => cfgRef.current);
    backendRef.current = be;
    be.start();
    return () => be.stop();
  }, []);

  // ----- apply accent -----
  useEffect(() => {
    const r = document.documentElement;
    const a = ACCENTS[t.accent] || { bright: t.accent, dim: t.accent };
    r.style.setProperty("--orange", t.accent);
    r.style.setProperty("--orange-bright", a.bright);
    r.style.setProperty("--orange-dim", a.dim);
    r.style.setProperty("--orange-wash", hexA(t.accent, 0.12));
    r.style.setProperty("--orange-line", hexA(t.accent, 0.35));
  }, [t.accent]);

  // ----- motion multiplier -----
  useEffect(() => {
    document.documentElement.style.setProperty("--mo", String(0.4 + (t.motion / 10) * 1.6));
  }, [t.motion]);

  // ----- circuit breaker override -----
  useEffect(() => { BX.actions.setCircuitBreakerState(t.circuit); }, [t.circuit]);

  // ----- feed rate -----
  useEffect(() => { if (backendRef.current) backendRef.current.setRate(); }, [t.feed]);

  const densityClass = t.density === "compact" ? "density-compact" : t.density === "comfortable" ? "density-comfortable" : "";
  const motionClass = t.motion === 0 ? "motion-off" : "";

  const onNav = useCallback((id) => {
    setActive(id);
    const map = { top: "bx-top", prices: "bx-prices", zscore: "bx-zscore", feed: "bx-feed", trades: "bx-trades" };
    const el = document.getElementById(map[id]);
    const cont = contentRef.current;
    if (!el || !cont) return;
    const target = id === "top" ? 0 : Math.max(0, cont.scrollTop + el.getBoundingClientRect().top - cont.getBoundingClientRect().top - 12);
    // animate with setInterval (rAF / native smooth-scroll are paused when the preview iframe is backgrounded)
    const start = cont.scrollTop, dist = target - start, dur = 360, t0 = Date.now();
    clearInterval(scrollIvRef.current);
    scrollIvRef.current = setInterval(() => {
      const p = Math.min(1, (Date.now() - t0) / dur);
      cont.scrollTop = start + dist * (1 - Math.pow(1 - p, 3));
      if (p >= 1) clearInterval(scrollIvRef.current);
    }, 16);
  }, []);

  return (
    <div className={"bx-app " + densityClass + " " + motionClass}>
      <NavRail active={active} onNav={onNav} />
      <div className="bx-main">
        <StatusBar />
        <div className="bx-content" ref={contentRef}>
          <div className="bx-cockpit">
            <div className="bx-col">
              <SpreadChart />
              <PnLChart />
            </div>
            <div className="bx-col">
              <PriceTable />
              <OpportunityFeed />
            </div>
          </div>
          <div className="bx-fullrow">
            <TradeHistory />
          </div>
        </div>
      </div>

      <TweaksPanel title="Tweaks">
        <TweakSection label="Demo" />
        <TweakRadio label="Circuit breaker" value={t.circuit}
          options={[{ value: "active", label: "Activo" }, { value: "watching", label: "Vigilando" }, { value: "paused", label: "Pausado" }]}
          onChange={(v) => setTweak("circuit", v)} />
        <TweakSlider label="Velocidad del feed" value={t.feed} min={1} max={10} step={1}
          onChange={(v) => setTweak("feed", v)} />
        <TweakSection label="Apariencia" />
        <TweakColor label="Acento" value={t.accent}
          options={["#F7931A", "#3B9EFF", "#7A5AE0", "#19C0C8"]}
          onChange={(v) => setTweak("accent", v)} />
        <TweakRadio label="Densidad" value={t.density}
          options={[{ value: "compact", label: "Compacta" }, { value: "regular", label: "Normal" }, { value: "comfortable", label: "Amplia" }]}
          onChange={(v) => setTweak("density", v)} />
        <TweakSlider label="Intensidad de motion" value={t.motion} min={0} max={10} step={1}
          onChange={(v) => setTweak("motion", v)} />
      </TweaksPanel>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
