import { useCallback, useEffect, useRef, useState } from 'react'
import {
  type LiveMessage,
  type TelemetryStatus,
  getTelemetryStatus,
  subscribeTelemetryLive,
} from '@/lib/telemetry'

const STATUS_POLL_MS = 5000

interface UseTelemetryStatusResult {
  status: TelemetryStatus | null
  loading: boolean
  error: string | null
  refetch: () => void
}

/** Polls GetTelemetryStatus every STATUS_POLL_MS -- "am I sending correct
 * data" is meant to update on its own, not just on a manual refresh. */
export function useTelemetryStatus(
  agentId: string,
  window?: string,
): UseTelemetryStatusResult {
  const [status, setStatus] = useState<TelemetryStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  const refetch = useCallback(() => setNonce((n) => n + 1), [])

  useEffect(() => {
    if (!agentId) return
    let cancelled = false
    setLoading(true)
    setError(null)

    function load() {
      getTelemetryStatus(agentId, window)
        .then((s) => {
          if (!cancelled) setStatus(s)
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            setError(err instanceof Error ? err.message : 'Failed to load telemetry status')
          }
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }

    load()
    const interval = setInterval(load, STATUS_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [agentId, window, nonce])

  return { status, loading, error, refetch }
}

export interface LiveRow extends LiveMessage {
  /** Monotonic id for React keys -- receivedAt alone can collide at high rates. */
  id: number
}

interface UseTelemetryLiveResult {
  rows: LiveRow[]
  connected: boolean
  error: string | null
  paused: boolean
  setPaused: (paused: boolean) => void
  clear: () => void
}

/** Subscribes to live telemetry for agentId, keeping a rolling buffer of at
 * most `capacity` rows (spec: ~500). Paused stops appending new rows without
 * closing the subscription, so resuming doesn't miss a reconnect. */
export function useTelemetryLive(agentId: string, capacity = 500): UseTelemetryLiveResult {
  const [rows, setRows] = useState<LiveRow[]>([])
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  useEffect(() => {
    pausedRef.current = paused
  }, [paused])
  const nextId = useRef(0)

  useEffect(() => {
    if (!agentId) return
    setRows([])
    setError(null)
    setConnected(true)

    const unsubscribe = subscribeTelemetryLive(
      agentId,
      (msg) => {
        if (pausedRef.current) return
        setRows((prev) => {
          const next = [...prev, { ...msg, id: nextId.current++ }]
          return next.length > capacity ? next.slice(next.length - capacity) : next
        })
      },
      (err) => {
        setConnected(false)
        setError(err.message)
      },
    )

    return () => {
      unsubscribe()
      setConnected(false)
    }
  }, [agentId, capacity])

  const clear = useCallback(() => setRows([]), [])

  return { rows, connected, error, paused, setPaused, clear }
}
