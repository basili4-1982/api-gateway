package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/basili4-1982/api-gateway/internal/config"
	"go.uber.org/zap"
)

// Manager связывает провайдер discovery со статическим конфигом: мёржит их и
// вызывает onUpdate только когда итоговый конфиг изменился.
type Manager struct {
	// mu защищает поля состояния менеджера. Не удерживается во время вызова
	// onUpdate (иначе Stop из колбэка приведёт к самоблокировке).
	mu       sync.Mutex
	base     *config.Config
	provider Provider
	onUpdate func(*config.Config) error
	log      *zap.Logger
	last     Result
	lastJSON string
	cancel   context.CancelFunc
	started  bool

	// applyMu сериализует всю последовательность merge→validate→dedup→onUpdate,
	// чтобы onUpdate вызывался строго по одному и по порядку, а lastJSON всегда
	// соответствовал последнему доставленному конфигу.
	applyMu sync.Mutex
}

// NewManager создаёт менеджер discovery. Если discovery выключен — возвращает
// менеджер с nil-провайдером (Start/Stop безопасны). onUpdate должен вернуть
// ошибку, если применение конфига не удалось: тогда оно будет повторено при
// следующем синке.
func NewManager(cfg *config.Config, log *zap.Logger, onUpdate func(*config.Config) error) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("discovery: config is nil")
	}
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
// Повторный Start, пока провайдер уже запущен, — no-op.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	provider := m.provider
	if provider == nil {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.started = true
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
	m.mu.Unlock()
	m.apply()
}

// Stop останавливает discovery. Остановка терминальна: dockerProvider.Stop
// необратим (повторный Start провайдера — no-op), поэтому started не
// сбрасывается и последующий Start менеджера тоже ничего не делает.
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
	m.apply()
}

func (m *Manager) apply() {
	// Сериализуем merge→validate→dedup→onUpdate целиком, чтобы SetBase (SIGHUP)
	// и onResult (горутина провайдера) не перемешивали lastJSON и не вызывали
	// onUpdate конкурентно. m.mu не удерживается во время колбэка.
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	// base и last читаются под m.mu в момент применения, а не снимаются
	// вызывающим: иначе устаревший снимок мог бы примениться последним.
	m.mu.Lock()
	base := m.base
	result := m.last
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
	m.mu.Unlock()

	if err := m.onUpdate(merged); err != nil {
		// Не фиксируем lastJSON: следующий синк повторит применение.
		m.log.Warn("discovery: onUpdate failed, will retry", zap.Error(err))
		return
	}

	m.mu.Lock()
	m.lastJSON = key
	m.mu.Unlock()
}
