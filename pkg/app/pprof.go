package app

import (
	"net/http"
	"net/http/pprof"
)

// pprofMux собирает pprof-обработчики на приватном mux: листенер по
// PPROF_ADDR обслуживает только их и не зависит от содержимого
// http.DefaultServeMux.
//
// Внимание: пакет net/http/pprof при любом импорте (не только blank)
// регистрирует свои обработчики на http.DefaultServeMux через init().
// Поэтому нигде в проекте нельзя слушать на DefaultServeMux
// (ListenAndServe(addr, nil)) — это опубликует pprof.
func pprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
