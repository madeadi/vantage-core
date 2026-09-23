import { Link } from 'react-router-dom'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { useAgents } from '@/hooks/use-agents'

/** /telemetry: pick an agent to check its contract & health, watch it live,
 * or replay a time range (spec Step 14). */
export function TelemetryIndexPage() {
  const { agents, loading, error } = useAgents()

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Telemetry</h1>
        <p className="text-muted-foreground text-sm">
          Pick an agent to check its contract &amp; health, watch live telemetry, or
          replay a time range.
        </p>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load agents</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
        </div>
      ) : agents.length === 0 ? (
        <p className="text-muted-foreground text-sm">No agents provisioned.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {agents.map((agent) => (
            <Link
              key={agent.id}
              to={`/telemetry/${agent.id}`}
              className="flex flex-col gap-1 rounded-lg border border-border p-4 transition-colors hover:bg-muted"
            >
              <span className="font-medium">{agent.name}</span>
              <span className="text-muted-foreground font-mono text-xs">
                {agent.id}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
