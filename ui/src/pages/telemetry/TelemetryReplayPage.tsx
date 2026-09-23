import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Pause, Play, RotateCcw } from 'lucide-react'
import { cn } from 'cn'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { queryTelemetry, type TelemetryPoint } from '@/lib/telemetry'
import { TelemetryTable } from './TelemetryTable'

const SPEEDS = [1, 5, 30] as const
type Speed = (typeof SPEEDS)[number]

// Bounds how long replay waits between two points with an unusually large
// gap (a quiet period in the data), and how fast it will ever step even at
// the lowest speed -- both keep playback responsive rather than either
// stalling on a gap or flickering on back-to-back timestamps.
const MAX_STEP_MS = 3000
const MIN_STEP_MS = 30

function toDatetimeLocal(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const CAPACITY = 500

/** /telemetry/:agentId/replay: pick a time range, then play/pause/scrub/
 * speed through it, driving the same TelemetryTable renderer as Live so the
 * two can't drift (spec Step 14). Ordered by received_at (broker order,
 * immune to agent clock skew); the displayed time is the mapped `time`. */
export function TelemetryReplayPage() {
  const { agentId = '' } = useParams<{ agentId: string }>()

  const [from, setFrom] = useState(() => toDatetimeLocal(new Date(Date.now() - 60 * 60 * 1000)))
  const [to, setTo] = useState(() => toDatetimeLocal(new Date()))

  const [points, setPoints] = useState<TelemetryPoint[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [truncated, setTruncated] = useState(false)

  const [cursor, setCursor] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState<Speed>(1)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    setPlaying(false)
    setCursor(0)
    try {
      const fromISO = new Date(from).toISOString()
      const toISO = new Date(to).toISOString()

      // Pull every page in range, up to a safety cap -- a replay window is
      // meant to be reviewed, not an unbounded export; MaxLimit pages'
      // worth (see cmd/core/telemetry/query) is already a lot of points to
      // scrub through by hand.
      const all: TelemetryPoint[] = []
      let pageToken = ''
      let pages = 0
      let more = false
      do {
        const res = await queryTelemetry({ agentId, from: fromISO, to: toISO, pageToken })
        all.push(...res.points)
        pageToken = res.nextPageToken
        pages += 1
        more = pageToken !== '' && pages < 10
      } while (more)

      // Replay order is received_at (broker-side, monotonic), not the
      // mapped `time` the query API sorts by -- a skewed agent clock must
      // not make the scrub bar jump backwards. The displayed time per row
      // is still the mapped `time` (see TelemetryTable).
      all.sort((a, b) => new Date(a.receivedAt).getTime() - new Date(b.receivedAt).getTime())

      setPoints(all)
      setTruncated(pageToken !== '' && pages >= 10)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load telemetry')
    } finally {
      setLoading(false)
    }
  }

  // Playback: schedule the next step after the real gap between this point
  // and the next, scaled by speed -- so replay feels like the original
  // pacing sped up, not a fixed-rate flip through frames.
  useEffect(() => {
    if (!playing) return
    if (cursor >= points.length - 1) {
      setPlaying(false)
      return
    }
    const cur = points[cursor]
    const next = points[cursor + 1]
    const gapMs = new Date(next.receivedAt).getTime() - new Date(cur.receivedAt).getTime()
    const stepMs = Math.min(MAX_STEP_MS, Math.max(MIN_STEP_MS, gapMs / speed))

    timerRef.current = setTimeout(() => setCursor((c) => c + 1), stepMs)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [playing, cursor, points, speed])

  const rows = useMemo(() => {
    const played = points.slice(0, cursor + 1)
    const windowed = played.length > CAPACITY ? played.slice(played.length - CAPACITY) : played
    return [...windowed].reverse().map((p, i) => ({
      id: `${cursor}-${i}`,
      time: new Date(p.time).getTime(),
      valid: p.valid,
      raw: JSON.stringify(p.payload),
    }))
  }, [points, cursor])

  const hasData = points.length > 0

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground text-xs">From</span>
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground text-xs">To</span>
          <input
            type="datetime-local"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
          />
        </label>
        <Button size="sm" onClick={load} disabled={loading}>
          {loading ? 'Loading…' : 'Load'}
        </Button>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load telemetry</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {truncated ? (
        <Alert>
          <AlertTitle>Range truncated</AlertTitle>
          <AlertDescription>
            This window has more points than replay loads at once. Narrow the time
            range to see the rest.
          </AlertDescription>
        </Alert>
      ) : null}

      {hasData ? (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPlaying((p) => !p)}
              disabled={cursor >= points.length - 1 && !playing}
            >
              {playing ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
              {playing ? 'Pause' : 'Play'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setPlaying(false)
                setCursor(0)
              }}
            >
              <RotateCcw className="size-3.5" />
              Restart
            </Button>
            <div className="flex gap-1">
              {SPEEDS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setSpeed(s)}
                  className={cn(
                    'h-7 rounded-md px-2 text-xs font-medium transition-colors',
                    speed === s
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted text-muted-foreground hover:text-foreground',
                  )}
                >
                  {s}×
                </button>
              ))}
            </div>
            <span className="text-muted-foreground ml-auto text-xs">
              {cursor + 1} / {points.length}
            </span>
          </div>

          <input
            type="range"
            min={0}
            max={Math.max(0, points.length - 1)}
            value={cursor}
            onChange={(e) => {
              setPlaying(false)
              setCursor(Number(e.target.value))
            }}
            className="w-full"
            aria-label="Scrub"
          />

          <TelemetryTable rows={rows} emptyMessage="No points in range." />
        </>
      ) : !loading ? (
        <p className="text-muted-foreground text-sm">
          Pick a time range and press Load to fetch telemetry for replay.
        </p>
      ) : null}
    </div>
  )
}
