# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This file covers `ui/` — the VantageOS admin web app. The Go backend has its own guidance in the repo-root `../CLAUDE.md`.

## Commands

```bash
npm run dev      # Vite dev server on :5173 (proxies to backend, see below)
npm run build    # tsc -b (typecheck) then vite build → dist/
npm run lint     # oxlint (NOT eslint)
npm run preview  # serve the production build locally
```

There is no test runner configured.

## Backend dependency

The dev server proxies to two backend processes that must be running (`cd ..`):

- **core** (`cmd/core`) on `:8080` — serves Connect RPC under `/api.v1.*`, plus `/agents`, `/missions`, `/swagger`, and a legacy server-rendered SSE dashboard at `/ui/`.
- **embedded PocketBase** on `:8090` — auth + collections, proxied as `/api/*` and `/_/` (admin UI).

`make dev-core` from the repo root starts both. In a deployed setup the UI is served same-origin, so `pb.ts` uses a relative base URL (`/`) and no proxy is involved.

## Architecture

Early-stage scaffold: auth works end-to-end; most page components in `src/pages.tsx` are placeholders.

- **`src/main.tsx`** — mounts `BrowserRouter` → `AuthProvider` → `TooltipProvider` → `App`.
- **`src/App.tsx`** — if `useAuth().isValid` is false, renders `<Login />` for the entire app; otherwise mounts the router (`Layout` shell with sidebar + nested routes). This boolean gate is the only auth guard.
- **`src/auth.tsx`** — `AuthProvider` wraps the PocketBase auth store: `pb.authStore.onChange` pushes into React state so token refresh/expiry/logout anywhere re-render the app. `login()` calls `pb.collection('users').authWithPassword(identity, password)`.
- **`src/lib/pb.ts`** — the single shared `PocketBase` client. Import `pb` from here; do not construct new instances.

### Auth identity

The `users` collection accepts **either username or email** as the login identity. The `username` field and its index are added at runtime by core's PocketBase bootstrap hook (`../cmd/core/pocketbase.go` → `ensureUsernameAuth`), not by a migration in this repo. The login form submits a username.

## Conventions

- **Path alias**: `@/` → `src/` (configured in `vite.config.ts`, `tsconfig.app.json`, and `components.json`).
- **TypeScript**: `verbatimModuleSyntax` is on — use `import type { ... }` for type-only imports. `noUnusedLocals` / `noUnusedParameters` are enforced by the build.
- **UI components**: shadcn/ui (style `radix-nova`, base color `neutral`) built on `radix-ui` / `@base-ui/react`, in `src/components/ui/`. Add more with `npx shadcn@latest add <component>`. Icons: `lucide-react`.
- **Styling**: Tailwind v4 via `@tailwindcss/vite` (no `tailwind.config.js`; theme lives in `src/index.css`). Use the semantic tokens (`bg-background`, `text-muted-foreground`, `border-border`, …).
- **Routing**: `react-router-dom` v7. Sidebar nav is defined in `src/components/app-sidebar.tsx`; keep its entries in sync with the routes in `App.tsx`.


## Web API

Backend will use pocketbase API. 

API type is: src/lib/pocketbase-types.ts
