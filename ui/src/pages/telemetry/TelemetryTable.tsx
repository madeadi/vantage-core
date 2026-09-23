import { cn } from 'cn'

/** Common row shape for both Live and Replay -- spec Step 14: "Replay
 * drives the same renderer as live so the two cannot drift." */
export interface TelemetryRowData {
  id: string | number
  /** ms since epoch -- receive time for Live, the mapped `time` for Replay. */
  time: number
  /** undefined = no_contract. */
  valid?: boolean
  /** Payload as JSON text. */
  raw: string
}

function ValidityDot({ valid }: { valid?: boolean }) {
  if (valid === undefined) {
    return (
      <span
        title="No contract to validate against"
        className="bg-muted-foreground/50 inline-block size-2 rounded-full"
      />
    )
  }
  return (
    <span
      title={valid ? 'Valid' : 'Invalid'}
      className={cn(
        'inline-block size-2 rounded-full',
        valid ? 'bg-emerald-500' : 'bg-destructive',
      )}
    />
  )
}

export function TelemetryTable({
  rows,
  emptyMessage,
}: {
  rows: TelemetryRowData[]
  emptyMessage: string
}) {
  return (
    <div className="overflow-auto rounded-lg border border-border">
      <table className="w-full text-left text-sm">
        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
          <tr>
            <th className="w-8 px-3 py-2" />
            <th className="px-3 py-2">Time</th>
            <th className="px-3 py-2">Payload</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={3} className="text-muted-foreground px-3 py-6 text-center text-sm">
                {emptyMessage}
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr
                key={row.id}
                className={cn('border-t border-border', row.valid === false && 'bg-destructive/5')}
              >
                <td className="px-3 py-1.5">
                  <ValidityDot valid={row.valid} />
                </td>
                <td className="px-3 py-1.5 font-mono text-xs whitespace-nowrap">
                  {new Date(row.time).toLocaleTimeString()}
                </td>
                <td className="px-3 py-1.5 font-mono text-xs break-all">{row.raw}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}
