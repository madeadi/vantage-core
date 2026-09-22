import type { FC } from 'react'
import type { LatLngBoundsExpression } from 'leaflet'
import { MapContainer, TileLayer } from 'react-leaflet'
import 'leaflet/dist/leaflet.css'
import type { GisBound } from '@/lib/layouts'

interface LayoutMapProps {
  bound: GisBound
}

export const LayoutMap: FC<LayoutMapProps> = ({ bound }) => {
  const bounds = bound as LatLngBoundsExpression

  return (
    <MapContainer
      bounds={bounds}
      boundsOptions={{ padding: [24, 24] }}
      scrollWheelZoom
      className="h-full w-full"
    >
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
      />
    </MapContainer>
  )
}
