import { useEffect, useRef, useState } from 'react'
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
import { cn } from 'cn'
import {
  type CoordinateSystem,
  type Layout,
  type LayoutInput,
  createLayout,
  pixelFileURL,
  updateLayout,
} from '@/lib/layouts'

interface LayoutSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The layout being edited, or null to create a new one. */
  layout: Layout | null
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

const GIS_BOUND_PLACEHOLDER = `[
  [1.130474, 103.596000],
  [1.478400, 104.094500]
]`

export function LayoutSheet({ open, onOpenChange, layout, onSaved }: LayoutSheetProps) {
  const [name, setName] = useState('')
  const [coordinateSystem, setCoordinateSystem] = useState<CoordinateSystem | ''>('')
  const [gisBound, setGisBound] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [clearFile, setClearFile] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Reset the form whenever the sheet opens or the target layout changes.
  useEffect(() => {
    if (!open) return
    setName(layout?.name ?? '')
    setCoordinateSystem(layout?.coordinate_system ?? '')
    setGisBound(toJSONText(layout?.gis_bound))
    setFile(null)
    setClearFile(false)
    setError(null)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }, [open, layout])

  const existingFileURL = layout ? pixelFileURL(layout) : null
  const showPixel = coordinateSystem === 'pixel'
  const showLatlon = coordinateSystem === 'latlon'

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    let input: LayoutInput
    try {
      input = {
        name: name.trim(),
        coordinate_system: coordinateSystem,
        gis_bound: showLatlon ? parseJSONField('GIS bound', gisBound) : null,
        pixel_file: file ?? (clearFile ? '' : null),
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Invalid input')
      return
    }

    setSaving(true)
    try {
      if (layout) {
        await updateLayout(layout.id, input)
      } else {
        await createLayout(input)
      }
      onSaved()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save layout')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{layout ? 'Edit layout' : 'New layout'}</SheetTitle>
          <SheetDescription>
            {layout
              ? 'Update the layout and its coordinate configuration.'
              : 'Create a layout with a pixel image or a lat/lon bounding box.'}
          </SheetDescription>
        </SheetHeader>

        <form
          onSubmit={handleSubmit}
          className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4"
        >
          <div className="flex flex-col gap-2">
            <Label htmlFor="layout-name">Name</Label>
            <Input
              id="layout-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Warehouse Floor 1"
              autoFocus
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="layout-cs">Coordinate system</Label>
            <select
              id="layout-cs"
              value={coordinateSystem}
              onChange={(e) =>
                setCoordinateSystem(e.target.value as CoordinateSystem | '')
              }
              className={cn(
                'h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors',
                'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50',
                'dark:bg-input/30',
              )}
            >
              <option value="">Unset</option>
              <option value="pixel">Pixel</option>
              <option value="latlon">Lat/Lon</option>
            </select>
          </div>

          {showPixel ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="layout-file">Pixel image</Label>
              {existingFileURL && !file && !clearFile ? (
                <div className="flex items-center gap-3 rounded-lg border border-border p-2">
                  <img
                    src={existingFileURL}
                    alt={layout?.pixel_file ?? 'layout'}
                    className="size-12 rounded object-cover"
                  />
                  <span className="text-muted-foreground truncate font-mono text-xs">
                    {layout?.pixel_file}
                  </span>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="ml-auto"
                    onClick={() => setClearFile(true)}
                  >
                    Remove
                  </Button>
                </div>
              ) : null}
              <input
                id="layout-file"
                ref={fileInputRef}
                type="file"
                accept="image/jpeg,image/png,image/webp,image/apng"
                onChange={(e) => {
                  setFile(e.target.files?.[0] ?? null)
                  setClearFile(false)
                }}
                className={cn(
                  'w-full rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none transition-colors',
                  'file:mr-2 file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground',
                  'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
                )}
              />
              <p className="text-muted-foreground text-xs">
                JPEG, PNG, WebP or APNG. Leave blank to keep the current image.
              </p>
            </div>
          ) : null}

          {showLatlon ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="layout-gis">GIS bound</Label>
              <Textarea
                id="layout-gis"
                value={gisBound}
                onChange={(e) => setGisBound(e.target.value)}
                placeholder={GIS_BOUND_PLACEHOLDER}
                className="min-h-32 font-mono text-xs"
                spellCheck={false}
              />
              <p className="text-muted-foreground text-xs">
                JSON: <code>[[swLat, swLon], [neLat, neLon]]</code>. Leave blank for
                none.
              </p>
            </div>
          ) : null}

          {error ? <p className="text-destructive text-sm">{error}</p> : null}

          <SheetFooter className="px-0">
            <Button type="submit" disabled={saving}>
              {saving ? 'Saving…' : layout ? 'Save changes' : 'Create layout'}
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
