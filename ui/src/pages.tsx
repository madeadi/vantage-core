import { type ReactNode, useState } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { cn } from 'cn'
import { useAgents } from '@/hooks/use-agents'
import type { Agent } from '@/lib/agents'
import { useAgentGroups } from '@/hooks/use-agent-groups'
import { type AgentGroup, deleteAgentGroup } from '@/lib/agent-groups'
import { AgentGroupSheet } from '@/components/agent-group-sheet'
import { useLayouts } from '@/hooks/use-layouts'
import { type Layout, deleteLayout } from '@/lib/layouts'
import { LayoutSheet } from '@/components/layout-sheet'

function Page({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <div className="text-muted-foreground text-sm">
        {children ?? `${title} content goes here.`}
      </div>
    </div>
  )
}

export function DashboardPage() {
  return <Page title="Dashboard" />
}

export function MissionsPage() {
  return <Page title="Missions" />
}

export function TasksPage() {
  return <Page title="Tasks" />
}

export function VideoStreamPage() {
  return <Page title="Video Stream" />
}

export function RemoteControlPage() {
  return <Page title="Remote Control" />
}

function AgentRow({ agent }: { agent: Agent }) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-1">
        <span className="font-medium">{agent.name}</span>
        <span className="text-muted-foreground font-mono text-xs">{agent.id}</span>
      </div>
    </div>
  )
}

export function SettingsAgentsPage() {
  const { agents, loading, error, refetch } = useAgents()

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-lg font-medium">Agents</h2>
          <p className="text-muted-foreground text-sm">
            Agents provisioned in PocketBase.
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={refetch}
          disabled={loading}
        >
          <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
          Refresh
        </Button>
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
            <AgentRow key={agent.id} agent={agent} />
          ))}
        </div>
      )}
    </div>
  )
}

function LayoutRow({
  layout,
  onEdit,
  onDelete,
}: {
  layout: Layout
  onEdit: () => void
  onDelete: () => void
}) {
  const csLabel =
    layout.coordinate_system === 'pixel'
      ? 'Pixel'
      : layout.coordinate_system === 'latlon'
        ? 'Lat/Lon'
        : 'Unset'
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-1">
        <span className="font-medium">{layout.name || layout.id}</span>
        <span className="text-muted-foreground font-mono text-xs">
          {layout.id} · {csLabel}
        </span>
      </div>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" onClick={onEdit}>
          <Pencil className="size-3.5" />
          Edit
        </Button>
        <Button variant="destructive" size="sm" onClick={onDelete}>
          <Trash2 className="size-3.5" />
          Delete
        </Button>
      </div>
    </div>
  )
}

export function SettingsLayoutsPage() {
  const { layouts, loading, error, refetch } = useLayouts()
  const [sheetOpen, setSheetOpen] = useState(false)
  const [editing, setEditing] = useState<Layout | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Layout | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  function openCreate() {
    setEditing(null)
    setSheetOpen(true)
  }

  function openEdit(layout: Layout) {
    setEditing(layout)
    setSheetOpen(true)
  }

  async function confirmDelete() {
    if (!pendingDelete) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteLayout(pendingDelete.id)
      setPendingDelete(null)
      refetch()
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : 'Failed to delete layout')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-lg font-medium">Layouts</h2>
          <p className="text-muted-foreground text-sm">
            Maps agents localize against — a pixel image or a lat/lon bounding box.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={refetch} disabled={loading}>
            <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
            Refresh
          </Button>
          <Button size="sm" onClick={openCreate}>
            <Plus className="size-4" />
            New layout
          </Button>
        </div>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load layouts</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
        </div>
      ) : layouts.length === 0 ? (
        <p className="text-muted-foreground text-sm">No layouts yet.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {layouts.map((layout) => (
            <LayoutRow
              key={layout.id}
              layout={layout}
              onEdit={() => openEdit(layout)}
              onDelete={() => {
                setDeleteError(null)
                setPendingDelete(layout)
              }}
            />
          ))}
        </div>
      )}

      <LayoutSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        layout={editing}
        onSaved={refetch}
      />

      <AlertDialog
        open={pendingDelete != null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete layout?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently deletes{' '}
              <span className="font-medium text-foreground">
                {pendingDelete?.name || pendingDelete?.id}
              </span>
              . This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteError ? (
            <p className="text-destructive text-sm">{deleteError}</p>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                confirmDelete()
              }}
              disabled={deleting}
            >
              {deleting ? 'Deleting…' : 'Delete'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function AgentGroupRow({
  group,
  onEdit,
  onDelete,
}: {
  group: AgentGroup
  onEdit: () => void
  onDelete: () => void
}) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-1">
        <span className="font-medium">{group.name}</span>
        <span className="text-muted-foreground font-mono text-xs">{group.id}</span>
      </div>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" onClick={onEdit}>
          <Pencil className="size-3.5" />
          Edit
        </Button>
        <Button variant="destructive" size="sm" onClick={onDelete}>
          <Trash2 className="size-3.5" />
          Delete
        </Button>
      </div>
    </div>
  )
}

export function SettingsAgentGroupsPage() {
  const { groups, loading, error, refetch } = useAgentGroups()
  const [sheetOpen, setSheetOpen] = useState(false)
  const [editing, setEditing] = useState<AgentGroup | null>(null)
  const [pendingDelete, setPendingDelete] = useState<AgentGroup | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  function openCreate() {
    setEditing(null)
    setSheetOpen(true)
  }

  function openEdit(group: AgentGroup) {
    setEditing(group)
    setSheetOpen(true)
  }

  async function confirmDelete() {
    if (!pendingDelete) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteAgentGroup(pendingDelete.id)
      setPendingDelete(null)
      refetch()
    } catch (err) {
      setDeleteError(
        err instanceof Error ? err.message : 'Failed to delete agent group',
      )
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-lg font-medium">Agent groups</h2>
          <p className="text-muted-foreground text-sm">
            Group agents and share a telemetry schema and mapping across them.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={refetch} disabled={loading}>
            <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
            Refresh
          </Button>
          <Button size="sm" onClick={openCreate}>
            <Plus className="size-4" />
            New group
          </Button>
        </div>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load agent groups</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
          <Skeleton className="h-[74px] w-full" />
        </div>
      ) : groups.length === 0 ? (
        <p className="text-muted-foreground text-sm">No agent groups yet.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {groups.map((group) => (
            <AgentGroupRow
              key={group.id}
              group={group}
              onEdit={() => openEdit(group)}
              onDelete={() => {
                setDeleteError(null)
                setPendingDelete(group)
              }}
            />
          ))}
        </div>
      )}

      <AgentGroupSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        group={editing}
        onSaved={refetch}
      />

      <AlertDialog
        open={pendingDelete != null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete agent group?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently deletes{' '}
              <span className="font-medium text-foreground">
                {pendingDelete?.name}
              </span>
              . This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteError ? (
            <p className="text-destructive text-sm">{deleteError}</p>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                confirmDelete()
              }}
              disabled={deleting}
            >
              {deleting ? 'Deleting…' : 'Delete'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

export function NotFoundPage() {
  return <Page title="Not found">The page you are looking for does not exist.</Page>
}
