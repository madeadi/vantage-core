import { useCallback, useEffect, useState } from 'react'
import { type Layout, listLayouts } from '@/lib/layouts'

interface UseLayoutsResult {
  layouts: Layout[]
  loading: boolean
  error: string | null
  refetch: () => void
}

export function useLayouts(): UseLayoutsResult {
  const [layouts, setLayouts] = useState<Layout[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  const refetch = useCallback(() => setNonce((n) => n + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)

    listLayouts(controller.signal)
      .then((list) => setLayouts(list))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Failed to load layouts')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [nonce])

  return { layouts, loading, error, refetch }
}
