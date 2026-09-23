// Agent groups are stored in the embedded PocketBase `agent_groups` collection.
// A group carries a name plus an optional telemetry schema / mapping (both JSON).
// In dev Vite proxies `/api/*` to PocketBase on :8090; in a deployed setup the
// UI is served same-origin.
import { pb } from '@/lib/pb'

export interface AgentGroup {
  id: string
  name: string
  telemetry_schema: unknown
  telemetry_mapping: unknown
  created: string
  updated: string
}

export interface AgentGroupInput {
  name: string
  telemetry_schema: unknown
  telemetry_mapping: unknown
}

const COLLECTION = 'agent_groups'

export async function listAgentGroups(signal?: AbortSignal): Promise<AgentGroup[]> {
  return pb.collection(COLLECTION).getFullList<AgentGroup>({
    sort: 'name',
    requestKey: null,
    signal,
  })
}

export async function getAgentGroup(
  id: string,
  signal?: AbortSignal,
): Promise<AgentGroup> {
  return pb.collection(COLLECTION).getOne<AgentGroup>(id, { requestKey: null, signal })
}

export async function createAgentGroup(input: AgentGroupInput): Promise<AgentGroup> {
  return pb.collection(COLLECTION).create<AgentGroup>(input)
}

export async function updateAgentGroup(
  id: string,
  input: AgentGroupInput,
): Promise<AgentGroup> {
  return pb.collection(COLLECTION).update<AgentGroup>(id, input)
}

export async function deleteAgentGroup(id: string): Promise<void> {
  await pb.collection(COLLECTION).delete(id)
}
