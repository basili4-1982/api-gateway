package discovery

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/basili4-1982/api-gateway/internal/config"
	"go.uber.org/zap"
)

// Manager связывает провайдер discovery со статическим конфигом: мёржит их и
// вызывает onUpdate только когда итоговый конфиг изменился.
type Manager struct {
	mu       sync.Mutex
	base     *config.Config
	provider Provider
	onUpdate func(*config.Config)
	log      *zap.Logger
	last     Result
	lastJSON string
	cancel   context.CancelFunc
}

// NewManager создаёт менеджер discovery. Если discovery выключен — возвращает
// менеджер с nil-провайдером (Start/Stop безопасны).
func NewManager(cfg *config.Config, log *zap.Logger, onUpdate func(*config.Config)) (*Manager, error) {
	m := &Manager{base: cfg, onUpdate: onUpdate, log: log}
	if cfg.Discovery == nil || !cfg.Discovery.Enabled {
		return m, nil
	}
	d := cfg.Discovery
	provider, err := newDockerProvider(
		d.Host, d.APIVersion,
		ParseOptions{
			LabelPrefix:       d.LabelPrefix,
			ServiceNameLabels: d.ServiceNameLabels,
			DefaultTimeout:    d.DefaultTimeout,
			Network:           d.Network,
		},
		d.Debounce, d.ResyncInterval, log,
	)
	if err != nil {
		return nil, err
	}
	m.provider = provider
	return m, nil
}

// setProvider подменяет провайдер (для тестов).
func (m *Manager) setProvider(p Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = p
}

// Start запускает discovery. Безопасно вызывать при выключенном discovery.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	provider := m.provider
	m.mu.Unlock()
	if provider == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	go func() {
		if err := provider.Start(ctx, m.onResult); err != nil && ctx.Err() == nil {
			m.log.Warn("discovery: provider stopped", zap.Error(err))
		}
	}()
	return nil
}

// SetBase обновляет статический конфиг (например, после SIGHUP) и пересобирает
// итоговый конфиг с последним результатом discovery.
func (m *Manager) SetBase(cfg *config.Config) {
	m.mu.Lock()
	m.base = cfg
	result := m.last
	m.mu.Unlock()
	m.apply(result)
}

// Stop останавливает discovery.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cancel := m.cancel
	provider := m.provider
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if provider != nil {
		return provider.Stop()
	}
	return nil
}

func (m *Manager) onResult(result Result) {
	m.mu.Lock()
	m.last = result
	m.mu.Unlock()
	m.apply(result)
}

func (m *Manager) apply(result Result) {
	m.mu.Lock()
	base := m.base
	m.mu.Unlock()

	merged := config.Merge(base, result.Targets, result.Rules)
	if err := merged.Validate(); err != nil {
		m.log.Warn("discovery: merged config invalid, keeping previous", zap.Error(err))
		return
	}

	normalized, err := json.Marshal(merged)
	if err != nil {
		m.log.Warn("discovery: marshal merged config failed", zap.Error(err))
		return
	}
	key := string(normalized)

	m.mu.Lock()
	if key == m.lastJSON {
		m.mu.Unlock()
		return
	}
	m.lastJSON = key
	m.mu.Unlock()

	m.onUpdate(merged)
}
