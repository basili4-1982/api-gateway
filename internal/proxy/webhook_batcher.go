package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"code.sarnas.ru/api-gateway/internal/config"
	"go.uber.org/zap"
)

const (
	defaultBatchQueueSize = 8192
	webhookSendTimeout    = 10 * time.Second
)

// batchPayload — тело батч-POST: количество событий и сами события.
type batchPayload struct {
	Count  int          `json:"count"`
	Events []AuditEvent `json:"events"`
}

// webhookBatcher копит события одного HTTP-вебхука и отправляет их пачками:
// один POST на пачку вместо POST на каждое событие. Так объёмный аудит
// (событие на каждый запрос) не платит за лишний HTTP-раундтрип на запрос.
type webhookBatcher struct {
	wh        config.WebhookConfig
	client    *http.Client
	log       *zap.Logger
	metrics   *Metrics
	ch        chan AuditEvent
	flushInt  time.Duration
	wg        sync.WaitGroup
	saturated atomic.Bool
	dropped   atomic.Int64
}

func newWebhookBatcher(wh config.WebhookConfig, client *http.Client, log *zap.Logger, metrics *Metrics) *webhookBatcher {
	return newWebhookBatcherWithQueue(wh, client, log, metrics, defaultBatchQueueSize)
}

func newWebhookBatcherWithQueue(wh config.WebhookConfig, client *http.Client, log *zap.Logger, metrics *Metrics, queueSize int) *webhookBatcher {
	if queueSize < 1 {
		queueSize = 1
	}
	flush := wh.FlushInterval
	if flush <= 0 {
		flush = 200 * time.Millisecond
	}
	b := &webhookBatcher{
		wh:       wh,
		client:   client,
		log:      log,
		metrics:  metrics,
		ch:       make(chan AuditEvent, queueSize),
		flushInt: flush,
	}
	b.wg.Add(1)
	go b.run()
	return b
}

// enqueue кладёт событие в очередь, не блокируя запрос: переполненная очередь
// приёмника аудита не должна останавливать основной трафик шлюза (раньше
// постановка блокировалась, и лёгкий webhook endpoint каскадно вешал прокси —
// при этом гарантии «без потерь» всё равно не было, см. send). При полной
// очереди событие отбрасывается со счётчиком dropped и метрикой; о перегрузке
// сообщаем в лог один раз на эпизод, чтобы не спамить.
func (b *webhookBatcher) enqueue(ev AuditEvent) {
	select {
	case b.ch <- ev:
		if b.saturated.Swap(false) {
			b.log.Info("webhook queue drained, delivering again", zap.String("webhook", b.wh.Name))
		}
		return
	default:
	}

	if b.saturated.CompareAndSwap(false, true) {
		b.log.Error("webhook queue full: dropping audit events",
			zap.String("webhook", b.wh.Name),
			zap.Int("queue_size", cap(b.ch)),
		)
	}
	b.countDropped(1)
}

// countDropped фиксирует потерю n событий: локальный счётчик для диагностики
// и метрики шлюза (nil у конструируемых в тестах батчеров).
func (b *webhookBatcher) countDropped(n int64) {
	b.dropped.Add(n)
	if b.metrics != nil {
		b.metrics.IncWebhookDrops(b.wh.Name, n)
	}
}

func (b *webhookBatcher) run() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.flushInt)
	defer ticker.Stop()

	batch := make([]AuditEvent, 0, b.wh.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b.send(batch)
		batch = batch[:0]
	}

	for {
		select {
		case ev, ok := <-b.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= b.wh.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (b *webhookBatcher) send(events []AuditEvent) {
	data, err := json.Marshal(batchPayload{Count: len(events), Events: events})
	if err != nil {
		b.log.Error("failed to marshal webhook batch", zap.Error(err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookSendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.wh.WebhookURL, bytes.NewReader(data))
	if err != nil {
		b.log.Error("failed to create webhook batch request", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := b.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// Пачка теряется: фиксируем в счётчике и метрике, чтобы «тихая»
		// недоставка аудита была видна в мониторинге.
		b.countDropped(int64(len(events)))
		b.log.Error("failed to send webhook batch, events lost",
			zap.Error(err),
			zap.String("url", b.wh.WebhookURL),
			zap.Int("events", len(events)),
		)
		return
	}
	resp.Body.Close()
}

func (b *webhookBatcher) close() {
	close(b.ch)
	b.wg.Wait()
}
