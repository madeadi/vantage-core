import { useParams } from 'react-router-dom'
import { AlertTriangle, RefreshCw } from 'lucide-react'
import { cn } from 'cn'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useTelemetryStatus } from '@/hooks/use-telemetry'
import type { TelemetryViolationSummary } from '@/lib/telemetry'

function formatRelative(iso?: string): string {
  if (!iso) return 'Never'
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 0) return 'just now'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

function ViolationRow({ violation }: { violation: TelemetryViolationSummary }) {
  const isMismatch = violation.kind === 'schema_contract_mismatch'
  let sample: unknown = violation.sampleJson
  try {
    if (violation.sampleJson) sample = JSON.parse(violation.sampleJson)
  } catch {
    // leave as the raw string
  }

  return (
    <div
      className={cn(
        'flex flex-col gap-2 rounded-lg border p-4',
        isMismatch ? 'border-destructive/50 bg-destructive/5' : 'border-border',
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {isMismatch ? <AlertTriangle className="text-destructive size-4" /> : null}
          <span className={cn('font-mono text-xs font-medium', isMismatch && 'text-destructive')}>
            {violation.kind}
          </span>
          {violation.paths.length > 0 ? (
            <span className="text-muted-foreground font-mono text-xs">
              {violation.paths.join(', ')}
            </span>
          ) : null}
        </div>
        <span className="text-muted-foreground text-xs">
          ×{violation.count} · last {formatRelative(violation.lastSeen)}
        </span>
      </div>
      <p className="text-sm">{violation.detail}</p>
      {violation.sampleJson ? (
        <pre className="max-h-40 overflow-auto rounded-md bg-muted p-2 text-xs">
          {JSON.stringify(sample, null, 2)}
        </pre>
      ) : null}
    </div>
  )
}

/** /telemetry/:agentId/contract: just the recent contract-violation list --
 * "am I sending correct data" (spec Step 14). */
export function ContractHealthPage() {
  const { agentId = '' } = useParams<{ agentId: string }>()
  const { status, loading, error, refetch } = useTelemetryStatus(agentId)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={refetch} disabled={loading}>
          <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
          Refresh
        </Button>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load telemetry status</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loading && !status ? (
        <Skeleton className="h-[280px] w-full" />
      ) : status ? (
        status.recentViolations.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            No violations recorded in the last hour.
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {status.recentViolations.map((v, i) => (
              <ViolationRow key={`${v.kind}-${i}`} violation={v} />
            ))}
          </div>
        )
      ) : null}
    </div>
  )
}
