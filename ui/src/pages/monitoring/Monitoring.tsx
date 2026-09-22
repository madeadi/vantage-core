import { type FC, useState } from 'react'
import { PanelRight, RefreshCw } from 'lucide-react'
import { cn } from 'cn'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useLayouts } from '@/hooks/use-layouts'
import { parseGisBound, pixelFileURL } from '@/lib/layouts'
import { LayoutMap } from './LayoutMap'
import { PixelMap } from './PixelMap'
import { EventsPanel, type LayoutEvent } from './EventsPanel'

const EVENTS_PANEL_KEY = 'monitoring:events-panel'

function readPanelPref(): boolean {
  try {
    return localStorage.getItem(EVENTS_PANEL_KEY) !== '0'
  } catch {
    return true
  }
}

export const Monitoring: FC = () => {
  const { layouts, loading, error, refetch } = useLayouts()

  // The user's pick; falls back to the first layout when unset or stale.
  const [pickedId, setPickedId] = useState<string | null>(null)
  const selected = layouts.find((l) => l.id === pickedId) ?? layouts[0] ?? null

  const [showEvents, setShowEvents] = useState(readPanelPref)

  function togglePanel() {
    setShowEvents((prev) => {
      const next = !prev
      try {
        localStorage.setItem(EVENTS_PANEL_KEY, next ? '1' : '0')
      } catch {
        // ignore — storage may be unavailable
      }
      return next
    })
  }

  // Event stream is not wired up yet; the panel renders its empty state.
  const events: LayoutEvent[] = []

  function renderMap() {
    if (!selected) return null

    if (selected.coordinate_system === 'latlon') {
      const bound = parseGisBound(selected.gis_bound)
      if (!bound) {
        return (
          <p className="text-muted-foreground text-sm">
            This layout has no valid GIS bound set.
          </p>
        )
      }
      return (
        <div className="h-[70vh] flex-1 overflow-hidden rounded-lg border border-border">
          <LayoutMap key={selected.id} bound={bound} />
        </div>
      )
    }

    if (selected.coordinate_system === 'pixel') {
      const url = pixelFileURL(selected)
      if (!url) {
        return (
          <p className="text-muted-foreground text-sm">
            This layout has no pixel image uploaded.
          </p>
        )
      }
      return (
        <div className="h-[70vh] flex-1 overflow-hidden rounded-lg border border-border">
          <PixelMap key={selected.id} url={url} />
        </div>
      )
    }

    return (
      <p className="text-muted-foreground text-sm">
        This layout has no coordinate system set.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load layouts</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <Skeleton className="h-[70vh] w-full" />
      ) : layouts.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No layouts yet. Add one under{' '}
          <span className="font-medium">Settings → Layouts</span>.
        </p>
      ) : (
        <>
          <div className="flex items-center gap-2">
            <select
              id="monitoring-layout"
              aria-label="Layout"
              value={selected?.id ?? ''}
              onChange={(e) => setPickedId(e.target.value)}
              className={cn(
                'h-8 min-w-0 flex-1 rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors sm:max-w-xs',
                'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
              )}
            >
              {layouts.map((l) => (
                <option key={l.id} value={l.id}>
                  {(l.name || l.id) +
                    (l.coordinate_system ? ` (${l.coordinate_system})` : '')}
                </option>
              ))}
            </select>
            <Button variant="outline" size="sm" onClick={refetch} disabled={loading}>
              <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
              Refresh
            </Button>
            {!showEvents ? (
              <Button
                variant="outline"
                size="sm"
                onClick={togglePanel}
                aria-label="Show events"
              >
                <PanelRight className="size-4" />
                Events
              </Button>
            ) : null}
          </div>

          <div className="flex flex-col gap-4 lg:flex-row">
            {renderMap()}
            {showEvents ? (
              <EventsPanel
                layoutName={selected ? selected.name || selected.id : ''}
                events={events}
                onClose={togglePanel}
              />
            ) : null}
          </div>
        </>
      )}
    </div>
  )
}
