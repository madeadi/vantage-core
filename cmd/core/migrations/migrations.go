// Package migrations holds the PocketBase schema/seed migrations for cmd/core.
//
// Files are named "<unix-ts>_<slug>.go" and self-register via m.Register in an
// init() func; the runner applies them in filename order and records each in the
// _migrations table so every migration runs exactly once per database.
//
// Import this package for its side effects from cmd/core, and call
// app.RunAppMigrations() after app.Bootstrap() (see cmd/core/pocketbase.go).
//
// To scaffold a new migration:
//
//	go run ./cmd/core -config cmd/core/core.config.yaml migrate create "add_something"
//
// In dev, schema edits made in the admin UI are auto-written here (Automigrate).
package migrations

// Collection names shared across migrations. Kept local to this package on
// purpose: a migration must keep doing the same thing forever, so it must not
// depend on constants elsewhere in the app that may later be renamed.
const (
	collUsers        = "users"
	collAgents       = "agents"
	collAgentGroups  = "agent_groups"
	collMissions     = "missions"
	collLayouts      = "layouts"
	collAgentLayouts = "agent_layouts"
)

// adminOnlyRule grants API access only to an authenticated user whose role is
// "admin". Superusers bypass collection rules entirely.
const adminOnlyRule = `@request.auth.role = "admin"`
