import { useEffect, useRef } from 'react'
import { useMarketStore } from '../store/marketStore'
import type { ServerEvent } from '../types/api'

function getWsUrl(): string {
  if (import.meta.env.VITE_WS_URL) return import.meta.env.VITE_WS_URL
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws`
}
const API_URL = import.meta.env.VITE_API_URL ?? ''
const MAX_BACKOFF = 30_000

export function useMarketSocket() {
  const store = useMarketStore()
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const attempt = useRef(0)
  const wsRef = useRef<WebSocket | null>(null)

  function fetchInitialState() {
    Promise.all([
      fetch(`${API_URL}/api/status`).then((r) => r.json()).catch(() => null),
      fetch(`${API_URL}/api/trades`).then((r) => r.json()).catch(() => null),
      fetch(`${API_URL}/api/spreads`).then((r) => r.json()).catch(() => null),
    ]).then(([status, trades, spreads]) => {
      if (status) store.setCircuitBreakerState(status.circuit_breaker_state)
      if (Array.isArray(trades)) store.setTrades(trades)
      if (Array.isArray(spreads)) store.setSpreads(spreads)
    })
  }

  function connect() {
    const ws = new WebSocket(getWsUrl())
    wsRef.current = ws

    ws.onopen = () => {
      attempt.current = 0
      store.setWsConnected(true)
      fetchInitialState()
    }

    ws.onmessage = (e) => {
      let ev: ServerEvent
      try { ev = JSON.parse(e.data) } catch { return }
      switch (ev.type) {
        case 'price_update':    store.setPrices(ev.data); break
        case 'price_snapshot':
          for (const raw of Object.values(ev.data)) store.setPrices(raw)
          break
        case 'opportunity':     store.addOpportunity(ev.data); break
        case 'trade_executed':  store.addTrade(ev.data); break
        case 'pnl_update':      store.setPnL(ev.data); break
        case 'circuit_breaker': store.setCircuitBreakerState(ev.data.state); break
        case 'spread_stats':    store.setSpreads(ev.data); break
        case 'latency_stats':   store.setLatency(ev.data.p50_us, ev.data.p99_us, ev.data.samples); break
      }
    }

    ws.onclose = ws.onerror = () => {
      store.setWsConnected(false)
      wsRef.current = null
      const delay = Math.min(1000 * 2 ** attempt.current, MAX_BACKOFF)
      attempt.current += 1
      reconnectTimer.current = setTimeout(connect, delay)
    }
  }

  useEffect(() => {
    connect()
    return () => {
      reconnectTimer.current && clearTimeout(reconnectTimer.current)
      wsRef.current?.close()
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
}
