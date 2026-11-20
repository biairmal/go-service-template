package httpx

import (
	"context"
	"net/http"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit/handler"
	"github.com/biairmal/go-sdk/lib/httpkit/middleware"
)

// Token claim that identity sets on client-credentials tokens, and its value.
const (
	claimScope    = "scope"
	scopeInternal = "internal"
)

// RequireServiceToken lets only a service token (scope "internal") through.
// A user token, or no claims at all, gets 403.
func RequireServiceToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := auth.ClaimsFromContext(r.Context()); ok {
			if v, _ := c.Get(claimScope); v == scopeInternal {
				next.ServeHTTP(w, r)
				return
			}
		}
		handler.WriteErrorResponse(w, http.StatusForbidden, errorz.Forbidden().WithMessage("service token required"))
	})
}

type adminCheckKey struct{}

// DenyUnchecked marks a request as needing a permission check. Handle then
// refuses it with 403 unless RequirePermission ran first, so a forgotten check
// on an admin route fails closed.
func DenyUnchecked(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminCheckKey{}, new(bool))))
	})
}

// RequirePermission is the go-sdk middleware of the same name, and also
// records that the check ran, for DenyUnchecked.
func RequirePermission(az authz.Authorizer, perm string) func(http.Handler) http.Handler {
	check := middleware.RequirePermission(az, perm)
	return func(next http.Handler) http.Handler {
		return check(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if done, ok := r.Context().Value(adminCheckKey{}).(*bool); ok {
				*done = true
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// unchecked reports whether the request needs a permission check that has not run.
func unchecked(r *http.Request) bool {
	done, ok := r.Context().Value(adminCheckKey{}).(*bool)
	return ok && !*done
}
