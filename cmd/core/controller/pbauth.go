package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/pocketbase/pocketbase/core"
)

// authenticatePB validates the PocketBase auth token (the same token the
// admin UI/API use) carried in header's Authorization: Bearer <token>, and
// returns the auth record it belongs to. Used by handlers that aren't
// PocketBase's own router (e.g. the ConnectRPC/SSE telemetry endpoints from
// specs/mqtt_telemetry.specs.md Step 13), which don't get PocketBase's
// collection-rule enforcement for free.
func authenticatePB(app core.App, header http.Header) (*core.Record, error) {
	token := bearerToken(header)
	if token == "" {
		return nil, errors.New("missing bearer token")
	}
	return app.FindAuthRecordByToken(token, core.TokenTypeAuth)
}

func bearerToken(header http.Header) string {
	tok, _ := strings.CutPrefix(header.Get("Authorization"), "Bearer ")
	return tok
}

// isAdmin reports whether r is a superuser (PocketBase's own _superusers
// collection bypasses every collection rule, per root CLAUDE.md) or holds
// the users.role "admin" value.
func isAdmin(r *core.Record) bool {
	return r.Collection().Name == core.CollectionNameSuperusers || r.GetString("role") == "admin"
}

// isOperatorOrAbove reports whether r can read telemetry -- admin or
// operator, per specs/mqtt_telemetry.specs.md Step 13 ("operator reads,
// admin changes settings" -- the latter is enforced by the existing
// telemetry_settings/agent_groups collection rules, not by this service).
func isOperatorOrAbove(r *core.Record) bool {
	return isAdmin(r) || r.GetString("role") == "operator"
}

// RequireOperatorInterceptor rejects any ConnectRPC call not carrying a
// valid PocketBase auth token for an operator- or admin-role user (or a
// superuser) -- Step 13's "Auth via the PocketBase token; operator reads".
// Apply via connect.WithInterceptors when constructing a service handler.
func RequireOperatorInterceptor(app core.App) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			record, err := authenticatePB(app, req.Header())
			if err != nil || !isOperatorOrAbove(record) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("operator or admin role required"))
			}
			return next(ctx, req)
		}
	}
}
