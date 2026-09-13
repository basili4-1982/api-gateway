// Command auth is a minimal benchmark-only HTTP responder.
//
// It stands in for both the external auth service (Nginx auth_request, Traefik
// forwardAuth) and the permission service used by the reported api-gateway scenarios.
// It always answers 200 with a fixed permissions payload, so the measurement captures
// the cost of the gateway's auth/permission round trip rather than any real
// authorization logic.
//
// Build with: go build ./helpers/auth
package main

import "net/http"

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id":1,"permissions":["a","b","c"]}`))
	})
	_ = http.ListenAndServe(":9000", nil)
}
