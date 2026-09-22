import { useCallback, useEffect, useState } from 'react'
import { type Agent, listAgents } from '@/lib/agents'

interface UseAgentsResult {
  agents: Agent[]
  loading: boolean
  error: string | null
  refetch: () => void
}

export function useAgents(): UseAgentsResult {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  const refetch = useCallback(() => setNonce((n) => n + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)

    listAgents(controller.signal)
      .then((list) => setAgents(list))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Failed to load agents')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [nonce])

  return { agents, loading, error, refetch }
}
