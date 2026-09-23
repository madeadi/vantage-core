// Client for core's TelemetryService (ConnectRPC, see
// ../../../proto/api/v1/telemetry.proto) and the GET /telemetry/live SSE
// endpoint (both cmd/core/controller/telemetry_*.go). There's no generated
// TypeScript client for the api.v1 services yet (see the other src/lib/*.ts
// files calling /agents, /missions directly), so this speaks the Connect
// protocol's plain unary-JSON form over fetch -- one POST per RPC, JSON
// body in and out, matching protobuf's JSON mapping (camelCase field
// names, Timestamp as an RFC3339 string, Struct as a plain object,
// google.protobuf.BoolValue as a bare true/false with the key omitted
// entirely when unset, a zero-valued scalar also omitted). Confirmed
// against a live core instance while building this client, not assumed.
import { pb } from '@/lib/pb'

export interface TelemetryPoint {
  agentId: string
  time: string // RFC3339
  receivedAt: string // RFC3339
  groupId: string
  schemaHash: string
  /** undefined = no_contract (no group schema to validate against). */
  valid?: boolean
  fields?: Record<string, unknown>
  payload: Record<string, unknown>
}

export interface TelemetryViolationSummary {
  kind: string
  detail: string
  count: number
  lastSeen: string
  /** Failing JSON Pointer path(s), e.g. "/battery_percent". */
  paths: string[]
  /** The offending sample payload, already JSON text (may be a JSON string
   * literal rather than an object -- see the proto field's doc comment). */
  sampleJson: string
}

export interface TelemetryStatus {
  agentId: string
  groupId: string
  schemaHash: string
  lastSeen?: string
  validCount: number
  invalidCount: number
  recentViolations: TelemetryViolationSummary[]
}

export interface QueryTelemetryParams {
  agentId: string
  from?: string
  to?: string
  limit?: number
  bucket?: string
  pageToken?: string
}

export interface QueryTelemetryResult {
  points: TelemetryPoint[]
  nextPageToken: string
}

export class TelemetryAPIError extends Error {
  code: string
  constructor(code: string, message: string) {
    super(message)
    this.code = code
    this.name = 'TelemetryAPIError'
  }
}

async function callTelemetryRPC<T>(method: string, body: unknown): Promise<T> {
  const res = await fetch(`/api.v1.TelemetryService/${method}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${pb.authStore.token}`,
    },
    body: JSON.stringify(body),
  })
  const text = await res.text()
  const json: unknown = text ? JSON.parse(text) : null

  if (!res.ok) {
    const err = json as { code?: string; message?: string } | null
    throw new TelemetryAPIError(
      err?.code ?? 'unknown',
      err?.message ?? `${method} failed with status ${res.status}`,
    )
  }
  return json as T
}

export async function queryTelemetry(
  params: QueryTelemetryParams,
): Promise<QueryTelemetryResult> {
  const resp = await callTelemetryRPC<{
    points?: TelemetryPoint[]
    nextPageToken?: string
  }>('QueryTelemetry', {
    agentId: params.agentId,
    from: params.from,
    to: params.to,
    limit: params.limit,
    bucket: params.bucket,
    pageToken: params.pageToken,
  })
  return { points: resp.points ?? [], nextPageToken: resp.nextPageToken ?? '' }
}

export async function getTelemetryStatus(
  agentId: string,
  window?: string,
): Promise<TelemetryStatus> {
  const resp = await callTelemetryRPC<Partial<TelemetryStatus>>('GetTelemetryStatus', {
    agentId,
    window,
  })
  return {
    agentId: resp.agentId ?? agentId,
    groupId: resp.groupId ?? '',
    schemaHash: resp.schemaHash ?? '',
    lastSeen: resp.lastSeen,
    validCount: resp.validCount ?? 0,
    invalidCount: resp.invalidCount ?? 0,
    recentViolations: (resp.recentViolations ?? []).map((v) => ({
      kind: v.kind ?? '',
      detail: v.detail ?? '',
      count: v.count ?? 0,
      lastSeen: v.lastSeen ?? '',
      paths: v.paths ?? [],
      sampleJson: v.sampleJson ?? '',
    })),
  }
}

export interface LiveMessage {
  /** Client-side arrival time (ms since epoch) -- the envelope itself has no
   * timestamp, only whatever fields the agent chose to include in payload. */
  receivedAt: number
  /** undefined = no_contract (see cmd/core/telemetry.go's liveEnvelope). */
  valid?: boolean
  payload: unknown
  /** The payload only, as raw JSON text (for a raw-view toggle). */
  raw: string
}

/**
 * Subscribes to GET /telemetry/live?agent_id=. Uses fetch + a manual SSE
 * parser rather than the native EventSource API, which cannot send an
 * Authorization header -- see cmd/core/controller/telemetry_live.go, which
 * requires one. Returns an unsubscribe function.
 *
 * Each SSE "data:" line is a small envelope --
 * {"valid"?: boolean, "payload": <the agent's telemetry JSON>} -- not the
 * bare payload, so the UI can mark each row valid/invalid (spec Step 14)
 * without re-validating client-side against the group's JSON Schema. See
 * cmd/core/telemetry.go's liveEnvelope.
 */
export function subscribeTelemetryLive(
  agentId: string,
  onMessage: (msg: LiveMessage) => void,
  onError: (err: Error) => void,
): () => void {
  const controller = new AbortController()

  void (async () => {
    try {
      const res = await fetch(`/telemetry/live?agent_id=${encodeURIComponent(agentId)}`, {
        headers: { Authorization: `Bearer ${pb.authStore.token}` },
        signal: controller.signal,
      })
      if (!res.ok || !res.body) {
        throw new Error(`live subscription failed with status ${res.status}`)
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        let sep: number
        while ((sep = buffer.indexOf('\n\n')) !== -1) {
          const rawEvent = buffer.slice(0, sep)
          buffer = buffer.slice(sep + 2)

          const dataLine = rawEvent.split('\n').find((line) => line.startsWith('data: '))
          if (!dataLine) continue
          const raw = dataLine.slice('data: '.length)

          let envelope: { valid?: boolean; payload?: unknown }
          try {
            envelope = JSON.parse(raw) as { valid?: boolean; payload?: unknown }
          } catch {
            continue // not valid JSON -- skip rather than crash the live view
          }
          onMessage({
            receivedAt: Date.now(),
            valid: envelope.valid,
            payload: envelope.payload,
            raw: JSON.stringify(envelope.payload),
          })
        }
      }
    } catch (err) {
      if (controller.signal.aborted) return
      onError(err instanceof Error ? err : new Error('live subscription failed'))
    }
  })()

  return () => controller.abort()
}
