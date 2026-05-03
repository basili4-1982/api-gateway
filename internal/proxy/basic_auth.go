package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

func basicAuthMiddleware(expectedUser, expectedPass string) Middleware {
	expectedUserHash := sha256Hex(expectedUser)
	expectedPassHash := sha256Hex(expectedPass)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || r.URL.Path == "/ready" || strings.HasPrefix(r.URL.Path, "/api") {
				next.ServeHTTP(w, r)
				return
			}

			u, p, ok := r.BasicAuth()
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			uHash := sha256Hex(u)
			pHash := sha256Hex(p)

			if subtle.ConstantTimeCompare([]byte(uHash), []byte(expectedUserHash)) != 1 ||
				subtle.ConstantTimeCompare([]byte(pHash), []byte(expectedPassHash)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			r.Header.Del("Authorization")
			next.ServeHTTP(w, r)
		})
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
