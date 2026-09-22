import { useCallback, useEffect, useState } from 'react'
import { type AgentGroup, listAgentGroups } from '@/lib/agent-groups'

interface UseAgentGroupsResult {
  groups: AgentGroup[]
  loading: boolean
  error: string | null
  refetch: () => void
}

export function useAgentGroups(): UseAgentGroupsResult {
  const [groups, setGroups] = useState<AgentGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  const refetch = useCallback(() => setNonce((n) => n + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)

    listAgentGroups(controller.signal)
      .then((list) => setGroups(list))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Failed to load agent groups')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [nonce])

  return { groups, loading, error, refetch }
}
