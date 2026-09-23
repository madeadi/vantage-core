import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom'
import { cn } from 'cn'
import { useAgents } from '@/hooks/use-agents'

const tabs = [
  { to: '', label: 'Live', end: true },
  { to: 'contract', label: 'Contract & health', end: false },
  { to: 'replay', label: 'Replay', end: false },
]

/** Shared shell for /telemetry/:agentId (live, the default), .../contract and
 * .../replay: an agent switcher plus tab nav, matching the spec's
 * nested-route layout (Step 14). */
export function TelemetryLayout() {
  const { agentId = '' } = useParams<{ agentId: string }>()
  const { agents } = useAgents()
  const navigate = useNavigate()
  const agent = agents.find((a) => a.id === agentId)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {agent?.name || agentId}
          </h1>
          <p className="text-muted-foreground font-mono text-xs">{agentId}</p>
        </div>
        <select
          aria-label="Agent"
          value={agentId}
          onChange={(e) => navigate(`/telemetry/${e.target.value}`)}
          className={cn(
            'h-8 min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors sm:max-w-xs',
            'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
          )}
        >
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name || a.id}
            </option>
          ))}
        </select>
      </div>

      <div className="flex gap-1 border-b border-border">
        {tabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            end={tab.end}
            className={({ isActive }) =>
              cn(
                '-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'border-primary text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground',
              )
            }
          >
            {tab.label}
          </NavLink>
        ))}
      </div>

      <Outlet />
    </div>
  )
}
