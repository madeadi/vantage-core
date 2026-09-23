import { useParams } from 'react-router-dom'
import { Pause, Play, Trash2 } from 'lucide-react'
import { cn } from 'cn'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useTelemetryLive } from '@/hooks/use-telemetry'
import { TelemetryTable } from './TelemetryTable'

const CAPACITY = 500

/** /telemetry/:agentId (the default tab): an SSE subscription with a rolling
 * capped buffer, each row marked valid/invalid, pause/resume (spec Step 14). */
export function TelemetryLivePage() {
  const { agentId = '' } = useParams<{ agentId: string }>()
  const { rows, connected, error, paused, setPaused, clear } = useTelemetryLive(
    agentId,
    CAPACITY,
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <span
          className={cn(
            'inline-flex items-center gap-1.5 text-sm',
            connected ? 'text-foreground' : 'text-muted-foreground',
          )}
        >
          <span
            className={cn(
              'size-2 rounded-full',
              connected ? 'bg-emerald-500' : 'bg-muted-foreground',
            )}
          />
          {connected ? 'Connected' : 'Disconnected'}
        </span>
        <span className="text-muted-foreground text-xs">
          {rows.length} / {CAPACITY} rows
        </span>
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setPaused(!paused)}>
            {paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />}
            {paused ? 'Resume' : 'Pause'}
          </Button>
          <Button variant="outline" size="sm" onClick={clear}>
            <Trash2 className="size-3.5" />
            Clear
          </Button>
        </div>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Live subscription failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <TelemetryTable
        rows={[...rows]
          .reverse()
          .map((row) => ({ id: row.id, time: row.receivedAt, valid: row.valid, raw: row.raw }))}
        emptyMessage={connected ? 'Waiting for telemetry…' : 'Not connected.'}
      />
    </div>
  )
}
