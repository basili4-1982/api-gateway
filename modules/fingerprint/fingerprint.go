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
	"sync"

	"code.sarnas.ru/api-gateway/pkg/plugin"
	"code.sarnas.ru/api-gateway/pkg/tlsfp"

	"github.com/quic-go/quic-go"
)

type ctxKey struct{}

// sourceGateway — значение заголовка источника: отпечаток посчитал шлюз.
const sourceGateway = "gateway"

// fpValues — готовые значения заголовков. READ ONLY: создаются один раз на
// соединение и только читаются дальше, поэтому не пулятся — пул потребовал бы
// вернуть срез, пока на него ещё смотрят заголовки.
type fpValues struct {
	ja4v []string
	ja3v []string
	srcv []string
}

// holder живёт в контексте соединения. Отпечаток появляется после первого
// чтения ClientHello, поэтому срезы собираются лениво — при первом запросе,
// когда рукопожатие уже прошло.
type holder struct {
	f    tlsfp.Fingerprinted
	once sync.Once
	v    fpValues
}

func (h *holder) values() *fpValues {
	h.once.Do(func() {
		if h.f == nil {
			return
		}
		ja4, ja3 := h.f.Fingerprint()
		if ja4 != "" {
			h.v.ja4v = []string{ja4}
		}
		if ja3 != "" {
			h.v.ja3v = []string{ja3}
		}
		if ja4 != "" || ja3 != "" {
			h.v.srcv = []string{sourceGateway}
		}
	})
	return &h.v
}

type module struct {
	enabled bool
	// quic — отпечатки HTTP/3 по адресу клиента: снимаются с Initial-пакета
	// до того, как quic-go его разберёт.
	quic    *tlsfp.QUICRegistry
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
	m.quic = tlsfp.NewQUICRegistry()
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

// WrapPacketConn оборачивает UDP: на Initial-пакете считается отпечаток,
// сам пакет уходит дальше без изменений.
func (m *module) WrapPacketConn(c net.PacketConn) net.PacketConn {
	if !m.enabled {
		return c
	}
	if m.quic == nil {
		m.quic = tlsfp.NewQUICRegistry()
	}
	return tlsfp.NewQUICListener(c, m.quic, nil)
}

// HTTP3ConnContext достаёт отпечаток, снятый с Initial-пакета, по адресу
// клиента. В QUIC нет рукопожатия TLS в том виде, в каком оно есть в TCP,
// поэтому ключ — адрес, а не соединение.
func (m *module) HTTP3ConnContext(ctx context.Context, c *quic.Conn) context.Context {
	if !m.enabled || m.quic == nil || c == nil {
		return ctx
	}
	ja4, ja3, ok := m.quic.Get(c.RemoteAddr().String())
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, &holder{v: fpValues{
		ja4v: []string{ja4}, ja3v: []string{ja3}, srcv: []string{sourceGateway},
	}})
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
	return context.WithValue(ctx, ctxKey{}, &holder{f: f})
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

		if h, ok := r.Context().Value(ctxKey{}).(*holder); ok {
			// values() один раз на соединение; дальше — только чтение.
			if v := h.values(); len(v.ja4v) > 0 || len(v.ja3v) > 0 {
				if len(v.ja4v) > 0 {
					r.Header[m.c4] = v.ja4v
				}
				if len(v.ja3v) > 0 {
					r.Header[m.c3] = v.ja3v
				}
				if len(v.srcv) > 0 {
					r.Header[m.cs] = v.srcv
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// newModule задаёт дефолты до Configure: модуль обязан работать и без секции
// plugins.fingerprint. Пустые имена превратились бы в запись заголовка "".
func newModule() *module {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source"}
	m.quic = tlsfp.NewQUICRegistry()
	m.c4 = http.CanonicalHeaderKey(m.header4)
	m.c3 = http.CanonicalHeaderKey(m.header3)
	m.cs = http.CanonicalHeaderKey(m.headerS)
	return m
}

func init() { plugin.Register(newModule()) }

// NewModule отдаёт настроенный модуль вне init: нужен стендам и тестам,
// которые собирают сервер руками.
func NewModule() *module { return newModule() }
