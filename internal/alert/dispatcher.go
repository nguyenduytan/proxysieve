// Package alert routes bounded operational events to configured webhook destinations.
package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/events"
	"github.com/nguyenduytan/proxysieve/internal/security"
	publicalert "github.com/nguyenduytan/proxysieve/pkg/alert"
	publicevent "github.com/nguyenduytan/proxysieve/pkg/event"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/retry"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const (
	queueCapacity  = 256
	workerCount    = 2
	maxAttempts    = 3
	attemptTimeout = 15 * time.Second
)

type Store interface {
	GetWebhook(context.Context, model.ID) (store.WebhookRecord, error)
	ListWebhooks(context.Context) ([]store.WebhookRecord, error)
	ListAlertRules(context.Context) ([]store.AlertRuleRecord, error)
	RecordWebhookDelivery(context.Context, publicalert.Delivery) error
}

type Resolver interface {
	LookupNetIP(context.Context, string) ([]netip.Addr, error)
}

type SecretResolver interface {
	Resolve(context.Context, secret.Ref) (secret.Value, error)
}

type doer func(*http.Request) (*http.Response, error)

type Sender struct {
	Resolver Resolver
	Policy   security.DestinationPolicy
	Secrets  SecretResolver
	do       doer
}

type task struct {
	webhook publicalert.Webhook
	event   publicevent.Event
}

type Stats struct {
	EventsDropped     uint64 `json:"events_dropped"`
	DeliveriesDropped uint64 `json:"deliveries_dropped"`
	Succeeded         uint64 `json:"succeeded"`
	Failed            uint64 `json:"failed"`
	LogFailures       uint64 `json:"log_failures"`
	QueuedEvents      int    `json:"queued_events"`
	QueuedDeliveries  int    `json:"queued_deliveries"`
}

type Dispatcher struct {
	bus        *events.Bus
	store      Store
	sender     Sender
	events     chan publicevent.Event
	deliveries chan task
	cancel     context.CancelFunc
	done       chan struct{}
	ready      chan struct{}
	readyOnce  sync.Once
	startOnce  sync.Once
	stopOnce   sync.Once
	eventDrop  atomic.Uint64
	taskDrop   atomic.Uint64
	succeeded  atomic.Uint64
	failed     atomic.Uint64
	logFailed  atomic.Uint64
}

func New(bus *events.Bus, store Store, sender Sender) (*Dispatcher, error) {
	if bus == nil || store == nil || sender.Resolver == nil {
		return nil, publicalert.ErrInvalid
	}
	return &Dispatcher{bus: bus, store: store, sender: sender, events: make(chan publicevent.Event, queueCapacity), deliveries: make(chan task, queueCapacity), done: make(chan struct{}), ready: make(chan struct{})}, nil
}

func (d *Dispatcher) Start(parent context.Context) {
	d.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		d.cancel = cancel
		go d.run(ctx)
		select {
		case <-d.ready:
		case <-parent.Done():
		}
	})
}

func (d *Dispatcher) Stop() {
	d.Start(context.Background())
	d.stopOnce.Do(d.cancel)
	<-d.done
}

func (d *Dispatcher) Stats() Stats {
	return Stats{EventsDropped: d.eventDrop.Load(), DeliveriesDropped: d.taskDrop.Load(), Succeeded: d.succeeded.Load(), Failed: d.failed.Load(), LogFailures: d.logFailed.Load(), QueuedEvents: len(d.events), QueuedDeliveries: len(d.deliveries)}
}

func (d *Dispatcher) Test(ctx context.Context, id model.ID) (publicalert.Delivery, error) {
	record, err := d.store.GetWebhook(ctx, id)
	if err != nil {
		return publicalert.Delivery{}, err
	}
	event := publicevent.Event{ID: model.NewID(), At: time.Now().UTC(), Type: "webhook.test", Severity: publicevent.Info, Source: "admin", TargetType: "webhook", TargetID: string(id)}
	return d.deliver(ctx, record.Webhook, event)
}

func (d *Dispatcher) run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(workerCount + 2)
	go func() { defer workers.Done(); d.subscribe(ctx) }()
	go func() { defer workers.Done(); d.route(ctx) }()
	for range workerCount {
		go func() { defer workers.Done(); d.work(ctx) }()
	}
	workers.Wait()
	close(d.done)
}

func (d *Dispatcher) subscribe(ctx context.Context) {
	for ctx.Err() == nil {
		stream := d.bus.Subscribe(ctx)
		if stream == nil {
			if retry.Wait(ctx, 1) != nil {
				return
			}
			continue
		}
		d.readyOnce.Do(func() { close(d.ready) })
		for event := range stream {
			select {
			case d.events <- event:
			default:
				d.eventDrop.Add(1)
			}
		}
	}
}

func (d *Dispatcher) route(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-d.events:
			// ponytail: read configs per event; add invalidated caching only if event volume makes this measurable.
			lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			rules, rulesErr := d.store.ListAlertRules(lookupCtx)
			webhooks, webhooksErr := d.store.ListWebhooks(lookupCtx)
			cancel()
			if rulesErr != nil || webhooksErr != nil {
				d.eventDrop.Add(1)
				continue
			}
			byID := make(map[model.ID]publicalert.Webhook, len(webhooks))
			for _, record := range webhooks {
				if record.Webhook.Enabled {
					byID[record.Webhook.ID] = record.Webhook
				}
			}
			selected := map[model.ID]bool{}
			for _, record := range rules {
				if !record.Rule.Matches(event.Type) {
					continue
				}
				for _, id := range record.Rule.WebhookIDs {
					selected[id] = true
				}
			}
			for id := range selected {
				webhook, ok := byID[id]
				if !ok {
					continue
				}
				select {
				case d.deliveries <- task{webhook: webhook, event: event}:
				default:
					d.taskDrop.Add(1)
				}
			}
		}
	}
}

func (d *Dispatcher) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-d.deliveries:
			_, _ = d.deliver(ctx, task.webhook, task.event)
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, webhook publicalert.Webhook, event publicevent.Event) (publicalert.Delivery, error) {
	var last publicalert.Delivery
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, attemptTimeout)
		last = d.sender.send(attemptCtx, webhook, event, attempt)
		cancelAttempt()
		logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		if d.store.RecordWebhookDelivery(logCtx, last) != nil {
			d.logFailed.Add(1)
		}
		cancel()
		if last.Success {
			d.succeeded.Add(1)
			return last, nil
		}
		if !retryable(last) || attempt == maxAttempts || retry.Wait(ctx, uint8(attempt)) != nil {
			break
		}
	}
	d.failed.Add(1)
	return last, errors.New("webhook delivery failed")
}

func retryable(delivery publicalert.Delivery) bool {
	return delivery.ErrorCode == "network" || delivery.ErrorCode == "timeout" || delivery.StatusCode == http.StatusTooManyRequests || delivery.StatusCode >= 500
}

func (s Sender) send(ctx context.Context, webhook publicalert.Webhook, event publicevent.Event, attempt int) publicalert.Delivery {
	started := time.Now()
	delivery := publicalert.Delivery{ID: model.NewID(), WebhookID: webhook.ID, EventID: event.ID, AttemptedAt: started.UTC(), Attempt: attempt}
	finish := func(code string, status int) publicalert.Delivery {
		delivery.Success = code == ""
		delivery.ErrorCode, delivery.StatusCode, delivery.DurationNS = code, status, max(0, time.Since(started).Nanoseconds())
		return delivery
	}
	if webhook.Validate() != nil || !event.Validate() {
		return finish("invalid", 0)
	}
	target, _ := url.Parse(webhook.URL)
	addresses, err := s.Resolver.LookupNetIP(ctx, target.Hostname())
	if err != nil || s.Policy.Allow(addresses) != nil {
		return finish("destination_denied", 0)
	}
	body, err := json.Marshal(struct {
		Version int               `json:"version"`
		Event   publicevent.Event `json:"event"`
	}{Version: 1, Event: event})
	if err != nil {
		return finish("encode", 0)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		return finish("invalid", 0)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ProxySieve/1 webhook")
	request.Header.Set("X-ProxySieve-Event-ID", string(event.ID))
	request.Header.Set("Idempotency-Key", string(event.ID))
	if webhook.SecretRef != "" {
		if s.Secrets == nil {
			return finish("secret_unavailable", 0)
		}
		value, resolveErr := s.Secrets.Resolve(ctx, webhook.SecretRef)
		if resolveErr != nil {
			return finish("secret_unavailable", 0)
		}
		key := value.Reveal()
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(body)
		request.Header.Set("X-ProxySieve-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		clear(key)
	}
	client, closeIdle := s.client(target, addresses)
	defer closeIdle()
	response, err := client(request)
	if err != nil || response == nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
			return finish("timeout", 0)
		}
		return finish("network", 0)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return finish("http_status", response.StatusCode)
	}
	return finish("", response.StatusCode)
}

func (s Sender) client(target *url.URL, addresses []netip.Addr) (doer, func()) {
	if s.do != nil {
		return s.do, func() {}
	}
	port := target.Port()
	if port == "" {
		port = "443"
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableCompression: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, requestedPort, err := net.SplitHostPort(address)
		if err != nil || requestedPort != port || !sameHost(host, target.Hostname()) {
			return nil, security.ErrDestinationDenied
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		var last error
		for _, ip := range addresses {
			connection, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			last = dialErr
		}
		return nil, last
	}}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("webhook redirect rejected") }}
	return client.Do, transport.CloseIdleConnections
}

func sameHost(left, right string) bool {
	if leftIP, err := netip.ParseAddr(left); err == nil {
		if rightIP, e := netip.ParseAddr(right); e == nil {
			return leftIP.Unmap() == rightIP.Unmap()
		}
	}
	return strings.EqualFold(strings.TrimSuffix(left, "."), strings.TrimSuffix(right, "."))
}
