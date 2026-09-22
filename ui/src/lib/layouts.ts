// Layouts are stored in the embedded PocketBase `layouts` collection. A layout
// carries a display `name`, a `coordinate_system` (`pixel` or `latlon`), an
// optional `pixel_file` image (for pixel layouts) and an optional `gis_bound`
// bounding box (for latlon layouts).
// In dev Vite proxies `/api/*` to PocketBase on :8090; in a deployed setup the
// UI is served same-origin.
import { pb } from '@/lib/pb'
import type { LayoutsResponse } from '@/lib/pocketbase-types'

export type CoordinateSystem = 'pixel' | 'latlon'

/** A `gis_bound` value: [[swLat, swLon], [neLat, neLon]]. */
export type GisBound = [[number, number], [number, number]]

function isCoordPair(v: unknown): v is [number, number] {
  return (
    Array.isArray(v) &&
    v.length >= 2 &&
    typeof v[0] === 'number' &&
    typeof v[1] === 'number' &&
    Number.isFinite(v[0]) &&
    Number.isFinite(v[1])
  )
}

/** Parse a layout's `gis_bound` into a validated [SW, NE] pair, or null. */
export function parseGisBound(value: unknown): GisBound | null {
  if (!Array.isArray(value) || value.length < 2) return null
  const [sw, ne] = value
  if (!isCoordPair(sw) || !isCoordPair(ne)) return null
  return [
    [sw[0], sw[1]],
    [ne[0], ne[1]],
  ]
}

export interface Layout {
  id: string
  name: string
  coordinate_system: CoordinateSystem | ''
  pixel_file: string
  gis_bound: unknown
}

export interface LayoutInput {
  name: string
  coordinate_system: CoordinateSystem | ''
  gis_bound: unknown
  /** A File to upload, '' to clear the existing file, or null to leave it unchanged. */
  pixel_file: File | '' | null
}

const COLLECTION = 'layouts'

function toLayout(r: LayoutsResponse): Layout {
  return {
    id: r.id,
    name: r.name ?? '',
    coordinate_system: (r.coordinate_system as CoordinateSystem | undefined) ?? '',
    pixel_file: (r.pixel_file as string | undefined) ?? '',
    gis_bound: r.gis_bound ?? null,
  }
}

/** URL of a layout's uploaded pixel image, or null if it has none. */
export function pixelFileURL(layout: Layout): string | null {
  if (!layout.pixel_file) return null
  return pb.files.getURL(
    { id: layout.id, collectionId: COLLECTION, collectionName: COLLECTION },
    layout.pixel_file,
  )
}

function buildPayload(input: LayoutInput): Record<string, unknown> {
  const data: Record<string, unknown> = {
    name: input.name,
    coordinate_system: input.coordinate_system,
    gis_bound: input.gis_bound ?? null,
  }
  if (input.pixel_file instanceof File) data.pixel_file = input.pixel_file
  else if (input.pixel_file === '') data.pixel_file = ''
  return data
}

export async function listLayouts(signal?: AbortSignal): Promise<Layout[]> {
  const list = await pb.collection(COLLECTION).getFullList<LayoutsResponse>({
    sort: 'name',
    requestKey: null,
    signal,
  })
  return list.map(toLayout)
}

export async function createLayout(input: LayoutInput): Promise<Layout> {
  const r = await pb.collection(COLLECTION).create<LayoutsResponse>(buildPayload(input))
  return toLayout(r)
}

export async function updateLayout(id: string, input: LayoutInput): Promise<Layout> {
  const r = await pb
    .collection(COLLECTION)
    .update<LayoutsResponse>(id, buildPayload(input))
  return toLayout(r)
}

export async function deleteLayout(id: string): Promise<void> {
  await pb.collection(COLLECTION).delete(id)
}
