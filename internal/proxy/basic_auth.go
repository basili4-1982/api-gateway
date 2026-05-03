package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

func basicAuthMiddleware(expectedUser, expectedPass string) Middleware {
	expectedUserHash := sha256Hex(expectedUser)
	expectedPassHash := sha256Hex(expectedPass)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || r.URL.Path == "/ready" {
				next.ServeHTTP(w, r)
				return
			}

			u, p, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			uHash := sha256Hex(u)
			pHash := sha256Hex(p)

			if subtle.ConstantTimeCompare([]byte(uHash), []byte(expectedUserHash)) != 1 ||
				subtle.ConstantTimeCompare([]byte(pHash), []byte(expectedPassHash)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
