package discovery

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// dockerProvider отслеживает контейнеры через Docker/Podman API и вызывает
// onResult при старте, по событиям (с дебаунсом) и по периодическому ре-синку.
type dockerProvider struct {
	client   *dockerClient
	opts     ParseOptions
	debounce time.Duration
	resync   time.Duration
	log      *zap.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

func newDockerProvider(host, apiVersion string, opts ParseOptions, debounce, resync time.Duration, log *zap.Logger) (*dockerProvider, error) {
	client, err := newDockerClient(host, apiVersion)
	if err != nil {
		return nil, err
	}
	return &dockerProvider{
		client:   client,
		opts:     opts,
		debounce: debounce,
		resync:   resync,
		log:      log,
	}, nil
}

func (p *dockerProvider) Start(ctx context.Context, onResult func(Result)) error {
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()

	p.sync(ctx, onResult)

	triggers := make(chan struct{}, 1)
	go p.watchEvents(ctx, triggers)

	var tickerC <-chan time.Time
	if p.resync > 0 {
		ticker := time.NewTicker(p.resync)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			return nil
		case <-triggers:
			stopTimer()
			timer = time.NewTimer(p.debounce)
			timerC = timer.C
		case <-timerC:
			stopTimer()
			p.sync(ctx, onResult)
		case <-tickerC:
			p.sync(ctx, onResult)
		}
	}
}

func (p *dockerProvider) sync(ctx context.Context, onResult func(Result)) {
	containers, err := p.client.listContainers(ctx, p.opts.LabelPrefix)
	if err != nil {
		p.log.Warn("discovery: list containers failed", zap.Error(err))
		return
	}
	onResult(ParseContainers(containers, p.opts))
}

// watchEvents подписывается на /events и шлёт триггер в triggers. При обрыве
// потока (закрытии или ошибке подписки) переподключается с backoff до отмены
// ctx и сигнализирует о необходимости ре-синка: обрыв трактуется как
// reconnect-and-resync, чтобы не ждать периодического тикера.
func (p *dockerProvider) watchEvents(ctx context.Context, triggers chan<- struct{}) {
	notify := func() {
		select {
		case triggers <- struct{}{}:
		default:
		}
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		ch, err := p.client.events(ctx)
		if err != nil {
			p.log.Warn("discovery: events subscribe failed", zap.Error(err))
			notify()
			if !sleepCtx(ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for action := range ch {
			p.log.Debug("discovery: container event", zap.String("action", action))
			notify()
		}
		notify()
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

func (p *dockerProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
