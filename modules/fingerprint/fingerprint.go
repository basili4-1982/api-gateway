// Package fingerprint прокидывает отпечаток TLS-клиента (JA4 и JA3) в заголовки
// запроса к апстриму.
//
// Модуль подключается при сборке xgate: если его нет в бинарнике, шлюз не
// оборачивает слушатель и не тратит на отпечатки ни байта.
package fingerprint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"code.sarnas.ru/api-gateway/pkg/plugin"
	"code.sarnas.ru/api-gateway/pkg/tlsfp"
)

type ctxKey struct{}

// sourceGateway — значение заголовка источника: отпечаток посчитал шлюз.
const sourceGateway = "gateway"

// fp — отпечатки одного соединения.
type fp struct {
	ja4 string
	ja3 string
}

type module struct {
	enabled bool
	header4 string
	header3 string
	headerS string
	// канонические имена считаются один раз: на горячем пути не должно быть
	// ни одной лишней аллокации от http.Header.Set
	c4 string
	c3 string
	cs string
}

func (*module) Name() string { return "fingerprint" }

// Configure читает plugins.fingerprint.* из конфига шлюза.
func (m *module) Configure(cfg map[string]string) error {
	m.header4 = cfg["ja4_header"]
	if m.header4 == "" {
		m.header4 = "X-JA4"
	}
	m.header3 = cfg["ja3_header"]
	if m.header3 == "" {
		m.header3 = "X-JA3"
	}
	m.headerS = cfg["source_header"]
	if m.headerS == "" {
		m.headerS = "X-FP-Source"
	}
	m.enabled = cfg["enabled"] != "false"
	m.c4 = http.CanonicalHeaderKey(m.header4)
	m.c3 = http.CanonicalHeaderKey(m.header3)
	m.cs = http.CanonicalHeaderKey(m.headerS)
	return nil
}

// WrapListener читает ClientHello до TLS и считает отпечатки.
func (m *module) WrapListener(ln net.Listener) net.Listener {
	if !m.enabled {
		return ln
	}
	return tlsfp.NewListener(ln, nil)
}

// ConnContext кладёт отпечаток в контекст соединения: один раз на соединение,
// без карт и мьютексов. Запросы берут готовое значение из контекста.
func (m *module) ConnContext(ctx context.Context, c net.Conn) context.Context {
	if !m.enabled {
		return ctx
	}
	tc, ok := c.(*tls.Conn)
	if !ok {
		return ctx
	}
	f, ok := tc.NetConn().(tlsfp.Fingerprinted)
	if !ok {
		return ctx
	}
	ja4, ja3 := f.Fingerprint()
	if ja4 == "" && ja3 == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, fp{ja4: ja4, ja3: ja3})
}

// Wrap проставляет заголовки. Заголовки от клиента вырезаются всегда: иначе
// любой пришлёт свой X-JA4 и выдаст себя за браузер.
func (m *module) Wrap(next http.Handler) http.Handler {
	// Заголовки пишутся прямо в карту по заранее канонизированным именам:
	// Header.Set на каждом запросе — это аллокация на каждое имя.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delete(r.Header, m.c4)
		delete(r.Header, m.c3)
		delete(r.Header, m.cs)

		if v, ok := r.Context().Value(ctxKey{}).(fp); ok {
			if v.ja4 != "" {
				r.Header[m.c4] = []string{v.ja4}
			}
			if v.ja3 != "" {
				r.Header[m.c3] = []string{v.ja3}
			}
			r.Header[m.cs] = []string{sourceGateway}
		}
		next.ServeHTTP(w, r)
	})
}

// newModule задаёт дефолты до Configure: модуль обязан работать и без секции
// plugins.fingerprint. Пустые имена превратились бы в запись заголовка "".
func newModule() *module {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source"}
	m.c4 = http.CanonicalHeaderKey(m.header4)
	m.c3 = http.CanonicalHeaderKey(m.header3)
	m.cs = http.CanonicalHeaderKey(m.headerS)
	return m
}

func init() { plugin.Register(newModule()) }
