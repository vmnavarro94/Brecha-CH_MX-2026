import { useEffect, useState } from 'react'
import { WifiOff } from 'lucide-react'
import { useMarketStore } from '../store/marketStore'

const styles = {
  banner: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--sp-2)',
    padding: '8px 14px',
    margin: '0 0 14px 0',
    background: 'rgba(255, 197, 61, 0.10)',
    border: '1px solid rgba(255, 197, 61, 0.35)',
    borderRadius: 'var(--r-lg)',
    color: 'var(--warn)',
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
    letterSpacing: '0.04em',
    animation: 'bx-blink 1.5s steps(1) infinite',
  } as React.CSSProperties,

  dots: {
    fontFamily: 'var(--font-mono)',
    color: 'var(--fg-3)',
    marginLeft: 'auto',
    fontSize: '11px',
  } as React.CSSProperties,
}

export default function ReconnectBanner() {
  const wsConnected = useMarketStore((s) => s.wsConnected)
  const [downSince, setDownSince] = useState<number | null>(null)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    if (!wsConnected && downSince === null) {
      setDownSince(Date.now())
    } else if (wsConnected) {
      setDownSince(null)
    }
  }, [wsConnected, downSince])

  useEffect(() => {
    if (wsConnected) return
    const iv = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(iv)
  }, [wsConnected])

  if (wsConnected) return null

  const elapsed = downSince ? Math.floor((now - downSince) / 1000) : 0

  return (
    <div style={styles.banner} role="status" aria-live="polite">
      <WifiOff size={14} strokeWidth={2} />
      <span>Conexión perdida · reconectando con backoff exponencial</span>
      <span style={styles.dots}>desconectado hace {elapsed}s</span>
    </div>
  )
}
