package middleware

import (
	"net/http"
	"strings"
)

const secret = "fixture-secret-token"

// Auth rejects requests that do not carry a valid bearer token.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		authorized := token == secret
		if !authorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
