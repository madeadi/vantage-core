import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  type AgentGroup,
  type AgentGroupInput,
  createAgentGroup,
  updateAgentGroup,
} from '@/lib/agent-groups'

interface AgentGroupSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The group being edited, or null to create a new one. */
  group: AgentGroup | null
  onSaved: () => void
}

function toJSONText(value: unknown): string {
  if (value == null || value === '') return ''
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/** Parse a JSON textarea. Empty input yields null; invalid input throws. */
function parseJSONField(label: string, text: string): unknown {
  const trimmed = text.trim()
  if (!trimmed) return null
  try {
    return JSON.parse(trimmed)
  } catch {
    throw new Error(`${label} is not valid JSON`)
  }
}

export function AgentGroupSheet({
  open,
  onOpenChange,
  group,
  onSaved,
}: AgentGroupSheetProps) {
  const [name, setName] = useState('')
  const [schema, setSchema] = useState('')
  const [mapping, setMapping] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Reset the form whenever the sheet opens or the target group changes.
  useEffect(() => {
    if (!open) return
    setName(group?.name ?? '')
    setSchema(toJSONText(group?.telemetry_schema))
    setMapping(toJSONText(group?.telemetry_mapping))
    setError(null)
  }, [open, group])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('Name is required')
      return
    }

    let input: AgentGroupInput
    try {
      input = {
        name: trimmedName,
        telemetry_schema: parseJSONField('Telemetry schema', schema),
        telemetry_mapping: parseJSONField('Telemetry mapping', mapping),
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Invalid input')
      return
    }

    setSaving(true)
    try {
      if (group) {
        await updateAgentGroup(group.id, input)
      } else {
        await createAgentGroup(input)
      }
      onSaved()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save agent group')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{group ? 'Edit agent group' : 'New agent group'}</SheetTitle>
          <SheetDescription>
            {group
              ? 'Update the group name and its telemetry configuration.'
              : 'Create a group with an optional telemetry schema and mapping.'}
          </SheetDescription>
        </SheetHeader>

        <form
          onSubmit={handleSubmit}
          className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4"
        >
          <div className="flex flex-col gap-2">
            <Label htmlFor="ag-name">Name</Label>
            <Input
              id="ag-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Warehouse Fleet"
              autoFocus
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="ag-schema">Telemetry schema</Label>
            <Textarea
              id="ag-schema"
              value={schema}
              onChange={(e) => setSchema(e.target.value)}
              placeholder="{ }"
              className="min-h-32 font-mono text-xs"
              spellCheck={false}
            />
            <p className="text-muted-foreground text-xs">JSON. Leave blank for none.</p>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="ag-mapping">Telemetry mapping</Label>
            <Textarea
              id="ag-mapping"
              value={mapping}
              onChange={(e) => setMapping(e.target.value)}
              placeholder="{ }"
              className="min-h-32 font-mono text-xs"
              spellCheck={false}
            />
            <p className="text-muted-foreground text-xs">JSON. Leave blank for none.</p>
          </div>

          {error ? <p className="text-destructive text-sm">{error}</p> : null}

          <SheetFooter className="px-0">
            <Button type="submit" disabled={saving}>
              {saving ? 'Saving…' : group ? 'Save changes' : 'Create group'}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Cancel
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}
