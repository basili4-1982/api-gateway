package proxy

import (
	"net/http"

	"code.sarnas.ru/api-gateway/pkg/tlsfp"
)

const (
	// headerJA3 — отпечаток, который шлюз передаёт апстриму.
	headerJA3 = "X-JA3"
	// headerJA3Source — кто именно посчитал отпечаток: шлюз или доверенный прокси.
	headerJA3Source = "X-JA3-Source"
	// ja3SourceGateway — значение headerJA3Source для нашего TLS-листенера.
	ja3SourceGateway = "gateway"
)

// ja3HeaderMiddleware кладёт отпечаток TLS-клиента в заголовок запроса к апстриму.
//
// Заголовок от клиента всегда вырезается: иначе любой мог бы прислать свой
// X-JA3 и выдать себя за браузер. Значение берётся только из реестра, который
// заполняет наш листенер после рукопожатия.
//
// Только первое рукопожатие: при keep-alive в одном соединении отпечатка нет,
// и заголовок не ставится. Апстрим должен трактовать отсутствие как «неизвестно»
// и не считать это признаком робота.
func ja3HeaderMiddleware(mp *MultiProxy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Del(headerJA3)
			r.Header.Del(headerJA3Source)

			if sum := mp.ja3For(r); sum != "" {
				r.Header.Set(headerJA3, sum)
				r.Header.Set(headerJA3Source, ja3SourceGateway)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ja3For читает отпечаток из реестра. Безопасен при nil: HTTP-режим и HTTP/3
// отпечатка не дают.
func (mp *MultiProxy) ja3For(r *http.Request) string {
	reg := mp.ja3.Load()
	if reg == nil {
		return ""
	}
	return reg.Get(r.RemoteAddr)
}

// withRegistry сохраняет реестр для middleware. Вызывается один раз при старте.
func (mp *MultiProxy) withRegistry(reg *tlsfp.Registry) { mp.ja3.Store(reg) }
