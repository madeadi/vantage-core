// Agents are read straight from the embedded PocketBase `agents` collection
// (see ../../cmd/core/pbconfig.go). In dev Vite proxies `/api/*` to PocketBase
// on :8090; in a deployed setup the UI is served same-origin.
import { pb } from '@/lib/pb'

export interface Agent {
  /** PocketBase record id. */
  recordId: string
  /** Stable agent identifier used by the control plane. */
  id: string
  name: string
}

interface AgentRecord {
  id: string
  agent_id: string
  name: string
}

export async function listAgents(signal?: AbortSignal): Promise<Agent[]> {
  const records = await pb.collection('agents').getFullList<AgentRecord>({
    sort: 'name',
    requestKey: null,
    signal,
  })
  return records.map((r) => ({
    recordId: r.id,
    id: r.agent_id,
    name: r.name || r.agent_id,
  }))
}
