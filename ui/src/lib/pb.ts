import PocketBase from 'pocketbase'

// The core server (cmd/core) runs an embedded PocketBase instance. In dev it
// listens on :8090 and Vite proxies `/api/*` to it (see vite.config.ts); in a
// deployed setup the UI is served from the same origin, so a relative base URL
// works in both cases.
export const pb = new PocketBase(import.meta.env.VITE_PB_URL ?? '/')
