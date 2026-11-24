// Package plugin is the compile-time module registry. A module registers itself
// from init(); xgate builds a custom main with blank imports.
package plugin

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

// Module describes an optional gateway module.
type Module interface {
	Name() string
}

// Configurable is implemented by modules that read runtime config from the
// `plugins:` section. Configure is called once at startup, before serving.
type Configurable interface {
	Module
	Configure(map[string]string) error
}

var (
	mu      sync.RWMutex
	modules []Module
)

func Register(m Module) {
	if m == nil || m.Name() == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	modules = append(modules, m)
}

func Modules() []Module {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Module, len(modules))
	copy(out, modules)
	return out
}

// Configure applies the `plugins:` section to registered modules. Unknown
// module names are rejected so a typo is visible at startup.
func Configure(values map[string]map[string]string) error {
	if _, err := resolveConfigurable(values); err != nil {
		return err
	}
	// Порядок применения — порядок регистрации, чтобы старт был детерминированным.
	for _, m := range Modules() {
		c, ok := m.(Configurable)
		if !ok {
			continue
		}
		cfg, ok := values[m.Name()]
		if !ok {
			continue
		}
		if err := c.Configure(cfg); err != nil {
			return fmt.Errorf("plugins.%s: %w", m.Name(), err)
		}
	}
	return nil
}

// ValidateConfig проверяет имена модулей из `plugins:` без вызова Configure.
// Используется `gateway --check`, чтобы опечатка ловилась до запуска.
func ValidateConfig(values map[string]map[string]string) error {
	_, err := resolveConfigurable(values)
	return err
}

func resolveConfigurable(values map[string]map[string]string) (map[string]Configurable, error) {
	mods := Modules()
	byName := make(map[string]Module, len(mods))
	for _, m := range mods {
		byName[m.Name()] = m
	}
	out := make(map[string]Configurable, len(values))
	for name := range values {
		m, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("plugins.%s: module is not compiled", name)
		}
		c, ok := m.(Configurable)
		if !ok {
			return nil, fmt.Errorf("plugins.%s: module does not accept configuration", name)
		}
		out[name] = c
	}
	return out, nil
}

// HTTPMiddlewares returns compiled modules that wrap the public handler, in
// registration order. The first registered wraps outermost.
func HTTPMiddlewares() []HTTPMiddleware {
	mods := Modules()
	out := make([]HTTPMiddleware, 0, len(mods))
	for _, m := range mods {
		if mw, ok := m.(HTTPMiddleware); ok {
			out = append(out, mw)
		}
	}
	return out
}

// ListenerWrapper — модуль, которому нужен доступ к соединению до TLS.
// Оборачивает слушатель: реализация обязана быть прозрачной, иначе сломается
// рукопожатие. Если ни один модуль интерфейс не реализует, слушатель не
// оборачивается вовсе — горячий путь остаётся нетронутым.
type ListenerWrapper interface {
	Module
	WrapListener(net.Listener) net.Listener
}

// ConnContextHook — модуль, которому нужны данные соединения в контексте
// запроса. Вызывается один раз на соединение, не на запрос.
type ConnContextHook interface {
	Module
	ConnContext(ctx context.Context, c net.Conn) context.Context
}

// ListenerWrappers возвращает модули-обёртки слушателя в порядке регистрации.
func ListenerWrappers() []ListenerWrapper {
	var out []ListenerWrapper
	for _, m := range Modules() {
		if w, ok := m.(ListenerWrapper); ok {
			out = append(out, w)
		}
	}
	return out
}

// ConnContextHooks возвращает хуки контекста соединения в порядке регистрации.
func ConnContextHooks() []ConnContextHook {
	var out []ConnContextHook
	for _, m := range Modules() {
		if h, ok := m.(ConnContextHook); ok {
			out = append(out, h)
		}
	}
	return out
}

// WrapListener прогоняет слушатель через все модули, которым он нужен.
func WrapListener(ln net.Listener) net.Listener {
	for _, w := range ListenerWrappers() {
		ln = w.WrapListener(ln)
	}
	return ln
}

// ConnContext прогоняет контекст через все хуки соединения.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	for _, h := range ConnContextHooks() {
		ctx = h.ConnContext(ctx, c)
	}
	return ctx
}

// PacketConnWrapper — модуль, которому нужны датаграммы до QUIC. Реализация
// обязана возвращать пакет без изменений, иначе сломается весь HTTP/3.
type PacketConnWrapper interface {
	Module
	WrapPacketConn(net.PacketConn) net.PacketConn
}

// HTTP3ConnContextHook — модуль, которому нужен контекст QUIC-соединения.
type HTTP3ConnContextHook interface {
	Module
	HTTP3ConnContext(ctx context.Context, c *quic.Conn) context.Context
}

// WrapPacketConn прогоняет UDP через модули.
func WrapPacketConn(c net.PacketConn) net.PacketConn {
	for _, m := range Modules() {
		if w, ok := m.(PacketConnWrapper); ok {
			c = w.WrapPacketConn(c)
		}
	}
	return c
}

// HTTP3ConnContext прогоняет контекст QUIC-соединения через модули.
func HTTP3ConnContext(ctx context.Context, c *quic.Conn) context.Context {
	for _, m := range Modules() {
		if h, ok := m.(HTTP3ConnContextHook); ok {
			ctx = h.HTTP3ConnContext(ctx, c)
		}
	}
	return ctx
}
