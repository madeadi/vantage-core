package main

import (
	"log/slog"

	"vantageos-core/cmd/core/config"
	_ "vantageos-core/cmd/core/migrations" // register app DB migrations

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	pbcmd "github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

// setupPocketBase creates and bootstraps an embedded PocketBase instance and
// applies the app DB migrations in cmd/core/migrations (collections, API rules,
// the users username/role fields, dev seed data). Schema changes belong in a new
// migration file — scaffold one with, from the repo root:
//
//	go run ./cmd/core -config cmd/core/core.config.yaml migrate create "add_x"
//
// In dev, schema edits made in the admin UI are auto-written there (Automigrate).
//
// setupPocketBase does NOT start the HTTP server — call servePocketBase for that
// once the collections have been read.
func setupPocketBase(cfg config.PocketBaseConfig) (*pocketbase.PocketBase, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "pb_data"
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: dataDir,
	})

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Dir:         "cmd/core/migrations",
		Automigrate: true,
	})

	// `go run ./cmd/core -config ... superuser create <email> <pass>`
	app.RootCmd.AddCommand(pbcmd.NewSuperuserCommand(app))

	if err := app.Bootstrap(); err != nil {
		return nil, err
	}

	// Bootstrap only runs the system migrations; apply ours before any caller
	// reads a collection. apis.Serve re-runs these (a no-op once applied).
	if err := app.RunAppMigrations(); err != nil {
		return nil, err
	}

	bindUserRoleDefault(app)

	return app, nil
}

// servePocketBase serves the PocketBase admin UI (/_/) and REST API. It blocks,
// so callers should run it in its own goroutine.
func servePocketBase(app *pocketbase.PocketBase, listenAddr string) {
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8090"
	}
	slog.Info("PocketBase listening", "addr", listenAddr, "admin", "http://"+listenAddr+"/_/")
	if err := apis.Serve(app, apis.ServeConfig{
		HttpAddr:        listenAddr,
		ShowStartBanner: false,
	}); err != nil {
		slog.Error("pocketbase server stopped", "err", err)
	}
}

// bindUserRoleDefault defaults the "role" of a newly created user when none is
// supplied: the very first user becomes an admin, everyone after a viewer. This
// is runtime behavior, not schema, so it lives here rather than in a migration.
func bindUserRoleDefault(app core.App) {
	app.OnRecordCreate("users").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("role") == "" {
			n, err := e.App.CountRecords("users")
			if err != nil {
				return err
			}
			if n == 0 {
				e.Record.Set("role", "admin")
			} else {
				e.Record.Set("role", "viewer")
			}
		}
		return e.Next()
	})
}
