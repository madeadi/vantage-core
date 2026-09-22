import { type FC, useEffect, useState } from 'react'
import { CRS, type LatLngBoundsExpression } from 'leaflet'
import { ImageOverlay, MapContainer } from 'react-leaflet'
import 'leaflet/dist/leaflet.css'

interface PixelMapProps {
  /** URL of the layout's pixel image. */
  url: string
}

/**
 * Renders a pixel-coordinate layout: the image is placed on a simple (non-geo)
 * CRS so it can be panned and zoomed like a floor plan.
 */
export const PixelMap: FC<PixelMapProps> = ({ url }) => {
  const [loaded, setLoaded] = useState<{ url: string; w: number; h: number } | null>(
    null,
  )

  useEffect(() => {
    let cancelled = false
    const img = new Image()
    img.onload = () => {
      if (!cancelled) {
        setLoaded({ url, w: img.naturalWidth, h: img.naturalHeight })
      }
    }
    img.src = url
    return () => {
      cancelled = true
    }
  }, [url])

  const size = loaded?.url === url ? loaded : null

  if (!size) {
    return (
      <div className="text-muted-foreground flex h-full w-full items-center justify-center text-sm">
        Loading image…
      </div>
    )
  }

  const bounds: LatLngBoundsExpression = [
    [0, 0],
    [size.h, size.w],
  ]

  return (
    <MapContainer
      crs={CRS.Simple}
      bounds={bounds}
      boundsOptions={{ padding: [24, 24] }}
      minZoom={-5}
      maxZoom={4}
      scrollWheelZoom
      className="h-full w-full bg-muted"
    >
      <ImageOverlay url={url} bounds={bounds} />
    </MapContainer>
  )
}
