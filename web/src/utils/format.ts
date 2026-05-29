export function fmtUsd(n: number, dp = 2): string {
  return '$' + Math.abs(n).toLocaleString('es-MX', { minimumFractionDigits: dp, maximumFractionDigits: dp })
}

export function fmtSigned(n: number, dp = 2): string {
  const abs = fmtUsd(Math.abs(n), dp)
  return n >= 0 ? `+${abs}` : `−${abs}` // Unicode minus
}

export function fmtPct(frac: number): string {
  const pct = frac * 100
  return (pct >= 0 ? '+' : '') + pct.toFixed(3) + '%'
}

export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('es-MX', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

export function fmtHM(ms: number): string {
  return new Date(ms).toLocaleTimeString('es-MX', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
