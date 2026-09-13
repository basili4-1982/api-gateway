// Command webhook is the minimal benchmark-only event sink.
//
// It accepts any method/path, returns 200 immediately, and never reads or logs the
// request body. Keeping the sink cheap and payload-agnostic is what the batched HTTP
// webhook scenario needs: the gateway posts arrays of events and the sink only has to
// acknowledge them. This is the batched-aware sink (gwbench sink2/main.go); the older
// gwbench sink/main.go logged every request and returned a JSON body, which is not
// needed for throughput measurement.
//
// Build with: go build ./helpers/webhook
package main

import "net/http"

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	_ = http.ListenAndServe(":9000", nil)
}
