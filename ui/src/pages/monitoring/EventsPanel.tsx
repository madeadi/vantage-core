import type { FC } from 'react'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'

export interface LayoutEvent {
  id: string
  /** ISO timestamp. */
  at: string
  /** Short kind, e.g. "task", "telemetry", "pose", "alert". */
  kind: string
  message: string
}

interface EventsPanelProps {
  layoutName: string
  events: LayoutEvent[]
  onClose: () => void
}

export const EventsPanel: FC<EventsPanelProps> = ({
  layoutName,
  events,
  onClose,
}) => {
  return (
    <div className="flex h-[70vh] w-full flex-col rounded-lg border border-border lg:w-80">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <div className="min-w-0">
          <p className="text-sm font-medium">Events</p>
          <p className="text-muted-foreground truncate text-xs">{layoutName}</p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={onClose}
          aria-label="Hide events"
        >
          <X className="size-4" />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {events.length === 0 ? (
          <p className="text-muted-foreground p-3 text-sm">
            Waiting for events on this layout…
          </p>
        ) : (
          <ul className="divide-y divide-border">
            {events.map((e) => (
              <li key={e.id} className="flex flex-col gap-0.5 px-3 py-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    {e.kind}
                  </span>
                  <time className="text-muted-foreground shrink-0 font-mono text-[11px]">
                    {new Date(e.at).toLocaleTimeString()}
                  </time>
                </div>
                <span className="text-sm">{e.message}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
