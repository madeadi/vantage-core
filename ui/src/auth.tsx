import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import type { AuthRecord } from 'pocketbase'
import { pb } from './lib/pb'

interface AuthState {
  user: AuthRecord | null
  isValid: boolean
  login: (identity: string, password: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthState | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthRecord | null>(pb.authStore.record)

  useEffect(() => {
    // Keep React state in sync with the PocketBase auth store (covers token
    // refresh, expiry, and logout from anywhere in the app).
    return pb.authStore.onChange(() => {
      setUser(pb.authStore.record)
    })
  }, [])

  const login = useCallback(async (identity: string, password: string) => {
    await pb.collection('users').authWithPassword(identity, password)
  }, [])

  const logout = useCallback(() => {
    pb.authStore.clear()
  }, [])

  const value = useMemo<AuthState>(
    () => ({ user, isValid: pb.authStore.isValid, login, logout }),
    [user, login, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}
