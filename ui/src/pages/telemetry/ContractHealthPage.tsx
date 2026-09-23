import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { AlertTriangle, RefreshCw } from 'lucide-react'
import { cn } from 'cn'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useTelemetryStatus } from '@/hooks/use-telemetry'
import { type AgentGroup, getAgentGroup } from '@/lib/agent-groups'
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

function StatTile({
  label,
  value,
  tone = 'default',
  mono = false,
}: {
  label: string
  value: string
  tone?: 'default' | 'ok' | 'warn'
  mono?: boolean
}) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border p-4">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span
        className={cn(
          'text-lg font-semibold',
          mono && 'font-mono text-sm',
          tone === 'ok' && 'text-emerald-600 dark:text-emerald-400',
          tone === 'warn' && 'text-destructive',
        )}
      >
        {value}
      </span>
    </div>
  )
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

/** /telemetry/:agentId: the group contract, schema hash, last-seen,
 * valid/invalid rate, and a violation list -- "am I sending correct data"
 * (spec Step 14). */
export function ContractHealthPage() {
  const { agentId = '' } = useParams<{ agentId: string }>()
  const { status, loading, error, refetch } = useTelemetryStatus(agentId)
  const [group, setGroup] = useState<AgentGroup | null>(null)
  const [groupError, setGroupError] = useState<string | null>(null)

  useEffect(() => {
    if (!status?.groupId) {
      setGroup(null)
      return
    }
    let cancelled = false
    setGroupError(null)
    getAgentGroup(status.groupId)
      .then((g) => {
        if (!cancelled) setGroup(g)
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setGroupError(err instanceof Error ? err.message : 'Failed to load group contract')
        }
      })
    return () => {
      cancelled = true
    }
  }, [status?.groupId])

  const total = (status?.validCount ?? 0) + (status?.invalidCount ?? 0)
  const validRate = total > 0 ? Math.round(((status?.validCount ?? 0) / total) * 100) : null

  const mismatch = status?.recentViolations.find((v) => v.kind === 'schema_contract_mismatch')

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

      {mismatch ? (
        <Alert variant="destructive">
          <AlertTriangle className="size-4" />
          <AlertTitle>Declared schema does not match the group contract</AlertTitle>
          <AlertDescription>{mismatch.detail}</AlertDescription>
        </Alert>
      ) : null}

      {loading && !status ? (
        <Skeleton className="h-[280px] w-full" />
      ) : status ? (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <StatTile label="Last seen" value={formatRelative(status.lastSeen)} />
            <StatTile
              label="Valid / invalid (last hour)"
              value={
                total > 0
                  ? `${validRate}% · ${status.validCount}/${total}`
                  : 'No data yet'
              }
              tone={total === 0 ? 'default' : validRate === 100 ? 'ok' : 'warn'}
            />
            <StatTile
              label="Schema hash"
              value={status.schemaHash ? status.schemaHash.slice(0, 16) : 'No contract'}
              mono
            />
          </div>

          <div className="rounded-lg border border-border p-4">
            <h2 className="mb-2 text-sm font-medium">Group contract</h2>
            {!status.groupId ? (
              <p className="text-muted-foreground text-sm">
                This agent has no group assigned, so there is no contract to validate
                against.
              </p>
            ) : !status.schemaHash ? (
              <p className="text-muted-foreground text-sm">
                This agent's group has no telemetry schema set.
              </p>
            ) : groupError ? (
              <p className="text-destructive text-sm">{groupError}</p>
            ) : (
              <pre className="max-h-80 overflow-auto rounded-md bg-muted p-3 text-xs">
                {group
                  ? JSON.stringify(group.telemetry_schema, null, 2)
                  : 'Loading…'}
              </pre>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <h2 className="text-sm font-medium">Recent violations</h2>
            {status.recentViolations.length === 0 ? (
              <p className="text-muted-foreground text-sm">
                No violations recorded in the last hour.
              </p>
            ) : (
              status.recentViolations.map((v, i) => (
                <ViolationRow key={`${v.kind}-${i}`} violation={v} />
              ))
            )}
          </div>
        </>
      ) : null}
    </div>
  )
}
