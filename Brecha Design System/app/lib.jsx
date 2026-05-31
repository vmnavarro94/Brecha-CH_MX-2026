/* global React */
// Brecha — Lucide icon as a React component (renders real SVG)
function Icon({ name, size = 16, strokeWidth = 1.75, className = "", style = {} }) {
  const ref = React.useRef(null);
  React.useEffect(() => {
    const host = ref.current;
    if (!host || !window.lucide) return;
    host.innerHTML = "";
    const i = document.createElement("i");
    i.setAttribute("data-lucide", name);
    host.appendChild(i);
    window.lucide.createIcons({
      attrs: { width: size, height: size, "stroke-width": strokeWidth },
      nameAttr: "data-lucide",
    });
  }, [name, size, strokeWidth]);
  return (
    <span
      ref={ref}
      className={"bx-icon " + className}
      style={{ display: "inline-flex", width: size, height: size, ...style }}
    />
  );
}
window.Icon = Icon;
