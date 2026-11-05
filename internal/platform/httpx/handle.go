package httpx

import (
	"net/http"

	"github.com/biairmal/go-sdk/lib/httpkit/handler"
)

// Handle adapts a handler.Func to the go-sdk envelope: data → 200, an errorz
// error → its status. Use it for every route, so headers derived from errors
// (such as Retry-After, once a rate limiter exists) are added in one place.
func Handle(h handler.Func) http.HandlerFunc {
	return handler.Handle(h)
}
