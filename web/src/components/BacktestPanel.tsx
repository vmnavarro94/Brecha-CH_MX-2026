import { useState, useEffect, useRef } from 'react'
import { Play, Clock, BarChart2 } from 'lucide-react'

// --- Types ---

interface StrategyMetrics {
  TotalPnL: number
  Sharpe: number
  MaxDrawdown: number
  HitRate: number
  ProfitFactor: number
  TradeCount: number
}

interface RunResult {
  run_id: string
  started_at: string
  ended_at: string
  from: string
  to: string
  status: string
  metrics: Record<string, StrategyMetrics>
}

interface RunStatus {
  state: string
  progress: number
  current_ts: number
  run_id: string
}

interface RunRecord {
  ID: string
  StartedAt: string
  EndedAt: string
  FromTS: string
  ToTS: string
  StrategiesJSON: string
  MetricsJSON: string
  Status: string
}

// --- Styles ---

const styles = {
  panel: {
    background: 'var(--bg-surface)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-lg)',
    overflow: 'hidden',
    marginTop: '16px',
  } as React.CSSProperties,

  head: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 15px',
    borderBottom: '1px solid var(--line-faint)',
  } as React.CSSProperties,

  eyebrow: {
    display: 'flex',
    alignItems: 'center',
    gap: '5px',
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 500,
    letterSpacing: '0.09em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  body: {
    padding: '16px',
  } as React.CSSProperties,

  formRow: {
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: '12px',
    marginBottom: '14px',
    alignItems: 'flex-end',
  } as React.CSSProperties,

  fieldGroup: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '4px',
  } as React.CSSProperties,

  label: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 500,
    letterSpacing: '0.07em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-2)',
  } as React.CSSProperties,

  input: {
    background: 'var(--bg)',
    border: '1px solid var(--line)',
    borderRadius: 'var(--r-sm)',
    color: 'var(--fg)',
    fontFamily: 'var(--font-mono)',
    fontSize: '12px',
    padding: '5px 8px',
    outline: 'none',
  } as React.CSSProperties,

  checkRow: {
    display: 'flex',
    gap: '12px',
    alignItems: 'center',
  } as React.CSSProperties,

  checkLabel: {
    display: 'flex',
    alignItems: 'center',
    gap: '5px',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg)',
    cursor: 'pointer',
  } as React.CSSProperties,

  runBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 'var(--r-sm)',
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    fontWeight: 600,
    letterSpacing: '0.07em',
    textTransform: 'uppercase' as const,
    padding: '6px 14px',
    cursor: 'pointer',
  } as React.CSSProperties,

  progressBar: {
    height: '4px',
    background: 'var(--line)',
    borderRadius: '2px',
    marginBottom: '12px',
    overflow: 'hidden',
  } as React.CSSProperties,

  progressFill: (pct: number): React.CSSProperties => ({
    height: '100%',
    width: `${Math.round(pct * 100)}%`,
    background: 'var(--accent)',
    transition: 'width 0.3s ease',
  }),

  statusLine: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg-2)',
    marginBottom: '12px',
  } as React.CSSProperties,

  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
  } as React.CSSProperties,

  th: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 600,
    letterSpacing: '0.07em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-2)',
    textAlign: 'left' as const,
    padding: '6px 8px',
    borderBottom: '1px solid var(--line-faint)',
  } as React.CSSProperties,

  td: {
    fontFamily: 'var(--font-mono)',
    fontSize: '11px',
    color: 'var(--fg)',
    padding: '6px 8px',
    borderBottom: '1px solid var(--line-faint)',
    cursor: 'pointer',
  } as React.CSSProperties,

  metricsSection: {
    marginTop: '16px',
    borderTop: '1px solid var(--line-faint)',
    paddingTop: '12px',
  } as React.CSSProperties,

  sectionTitle: {
    fontFamily: 'var(--font-mono)',
    fontSize: '10px',
    fontWeight: 600,
    letterSpacing: '0.09em',
    textTransform: 'uppercase' as const,
    color: 'var(--fg-2)',
    marginBottom: '8px',
  } as React.CSSProperties,
}

// --- Component ---

const ALL_STRATEGIES = ['spatial', 'triangular', 'funding']

function fmtPct(n: number): string {
  return (n * 100).toFixed(2) + '%'
}

function fmtNum(n: number, dp = 4): string {
  return n.toFixed(dp)
}

export default function BacktestPanel() {
  const nowIso = new Date().toISOString().slice(0, 16)
  const hourAgoIso = new Date(Date.now() - 3600_000).toISOString().slice(0, 16)

  const [from, setFrom] = useState(hourAgoIso)
  const [to, setTo] = useState(nowIso)
  const [speed, setSpeed] = useState('0')
  const [seed, setSeed] = useState('42')
  const [strategies, setStrategies] = useState<string[]>(['spatial', 'triangular'])

  const [runStatus, setRunStatus] = useState<RunStatus | null>(null)
  const [selectedResult, setSelectedResult] = useState<RunResult | null>(null)
  const [runs, setRuns] = useState<RunRecord[]>([])
  const [errorMsg, setErrorMsg] = useState('')

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Poll /api/backtest/status while running.
  const startPolling = (runId: string) => {
    if (pollRef.current) clearInterval(pollRef.current)
    pollRef.current = setInterval(async () => {
      try {
        const res = await fetch('/api/backtest/status')
        const data: RunStatus = await res.json()
        setRunStatus(data)
        if (data.state === 'done' || data.state === 'idle') {
          clearInterval(pollRef.current!)
          pollRef.current = null
          if (runId) {
            fetchResult(runId)
          }
          fetchRuns()
        }
      } catch {
        // ignore transient network errors
      }
    }, 500)
  }

  const fetchResult = async (runId: string) => {
    try {
      const res = await fetch(`/api/backtest/results/${runId}`)
      if (res.ok) {
        const data: RunResult = await res.json()
        setSelectedResult(data)
      }
    } catch {
      // ignore
    }
  }

  const fetchRuns = async () => {
    try {
      const res = await fetch('/api/backtest/runs')
      const data = await res.json()
      setRuns(data.runs ?? [])
    } catch {
      // ignore
    }
  }

  // Poll /api/backtest/runs every 5s.
  useEffect(() => {
    fetchRuns()
    const id = setInterval(fetchRuns, 5000)
    return () => clearInterval(id)
  }, [])

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  const toggleStrategy = (name: string) => {
    setStrategies(prev =>
      prev.includes(name) ? prev.filter(s => s !== name) : [...prev, name]
    )
  }

  const handleRun = async () => {
    setErrorMsg('')
    setSelectedResult(null)
    const body = {
      from: new Date(from).toISOString(),
      to: new Date(to).toISOString(),
      speed: parseFloat(speed) || 0,
      strategies,
      seed: parseInt(seed, 10) || 0,
    }
    try {
      const res = await fetch('/api/backtest/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await res.json()
      if (!res.ok) {
        setErrorMsg(data.error ?? 'Failed to start backtest')
        return
      }
      setRunStatus({ state: 'running', progress: 0, current_ts: 0, run_id: data.run_id })
      startPolling(data.run_id)
    } catch (e: unknown) {
      setErrorMsg(String(e))
    }
  }

  const isRunning = runStatus?.state === 'running'

  return (
    <div style={styles.panel} id="bx-backtest">
      <div style={styles.head}>
        <span style={styles.eyebrow}>
          <BarChart2 size={11} />
          Backtest
        </span>
      </div>

      <div style={styles.body}>
        {/* Controls */}
        <div style={styles.formRow}>
          <div style={styles.fieldGroup}>
            <label style={styles.label}>From</label>
            <input
              type="datetime-local"
              value={from}
              onChange={e => setFrom(e.target.value)}
              style={styles.input}
            />
          </div>
          <div style={styles.fieldGroup}>
            <label style={styles.label}>To</label>
            <input
              type="datetime-local"
              value={to}
              onChange={e => setTo(e.target.value)}
              style={styles.input}
            />
          </div>
          <div style={styles.fieldGroup}>
            <label style={styles.label}>Speed (0=max)</label>
            <input
              type="number"
              min="0"
              step="0.5"
              value={speed}
              onChange={e => setSpeed(e.target.value)}
              style={{ ...styles.input, width: '80px' }}
            />
          </div>
          <div style={styles.fieldGroup}>
            <label style={styles.label}>Seed</label>
            <input
              type="number"
              value={seed}
              onChange={e => setSeed(e.target.value)}
              style={{ ...styles.input, width: '80px' }}
            />
          </div>
        </div>

        <div style={{ ...styles.formRow, marginBottom: '16px' }}>
          <div style={styles.fieldGroup}>
            <label style={styles.label}>Strategies</label>
            <div style={styles.checkRow}>
              {ALL_STRATEGIES.map(name => (
                <label key={name} style={styles.checkLabel}>
                  <input
                    type="checkbox"
                    checked={strategies.includes(name)}
                    onChange={() => toggleStrategy(name)}
                  />
                  {name}
                </label>
              ))}
            </div>
          </div>

          <button
            style={styles.runBtn}
            onClick={handleRun}
            disabled={isRunning}
          >
            <Play size={11} />
            Run
          </button>
        </div>

        {errorMsg && (
          <div style={{ ...styles.statusLine, color: 'var(--red)' }}>{errorMsg}</div>
        )}

        {/* Progress bar */}
        {runStatus && (
          <>
            <div style={styles.progressBar}>
              <div style={styles.progressFill(runStatus.progress)} />
            </div>
            <div style={styles.statusLine}>
              <Clock size={10} style={{ marginRight: 4 }} />
              {runStatus.state} — {Math.round(runStatus.progress * 100)}%
            </div>
          </>
        )}

        {/* Metrics for selected run */}
        {selectedResult && selectedResult.metrics && (
          <div style={styles.metricsSection}>
            <div style={styles.sectionTitle}>Results — run {selectedResult.run_id.slice(0, 8)}</div>
            <table style={styles.table}>
              <thead>
                <tr>
                  <th style={styles.th}>Strategy</th>
                  <th style={styles.th}>Total PnL</th>
                  <th style={styles.th}>Sharpe</th>
                  <th style={styles.th}>Max DD</th>
                  <th style={styles.th}>Hit Rate</th>
                  <th style={styles.th}>Trades</th>
                </tr>
              </thead>
              <tbody>
                {Object.entries(selectedResult.metrics).map(([name, m]) => (
                  <tr key={name}>
                    <td style={styles.td}>{name}</td>
                    <td style={styles.td}>{fmtNum(m.TotalPnL, 2)}</td>
                    <td style={styles.td}>{fmtNum(m.Sharpe)}</td>
                    <td style={styles.td}>{fmtNum(m.MaxDrawdown, 2)}</td>
                    <td style={styles.td}>{fmtPct(m.HitRate)}</td>
                    <td style={styles.td}>{m.TradeCount}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Run history */}
        {runs.length > 0 && (
          <div style={styles.metricsSection}>
            <div style={styles.sectionTitle}>Run History</div>
            <table style={styles.table}>
              <thead>
                <tr>
                  <th style={styles.th}>ID</th>
                  <th style={styles.th}>Status</th>
                  <th style={styles.th}>Started</th>
                  <th style={styles.th}>Strategies</th>
                </tr>
              </thead>
              <tbody>
                {runs.map(run => (
                  <tr
                    key={run.ID}
                    onClick={() => fetchResult(run.ID)}
                    style={{ cursor: 'pointer' }}
                  >
                    <td style={styles.td}>{run.ID.slice(0, 8)}</td>
                    <td style={styles.td}>{run.Status}</td>
                    <td style={styles.td}>
                      {new Date(run.StartedAt).toLocaleString()}
                    </td>
                    <td style={styles.td}>
                      {(() => {
                        try { return JSON.parse(run.StrategiesJSON).join(', ') }
                        catch { return run.StrategiesJSON }
                      })()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
