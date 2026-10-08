// Package webhook delivers outgoing webhooks for domain events.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Dispatcher sends events to subscribed webhooks asynchronously.
type Dispatcher struct {
	store  *store.Store
	log    *slog.Logger
	client *http.Client
}

// New creates a dispatcher.
func New(s *store.Store, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: s, log: log, client: &http.Client{Timeout: 15 * time.Second}}
}

// Event is the webhook payload.
type Event struct {
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
}

// Emit sends an event to all matching webhooks in the background.
func (d *Dispatcher) Emit(event string, data any) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hooks, err := d.store.ListWebhooks(ctx, true)
		if err != nil {
			d.log.Error("webhook: list", "err", err)
			return
		}
		payload, _ := json.Marshal(Event{Event: event, Timestamp: time.Now().UTC().Format(time.RFC3339), Data: data})
		for _, h := range hooks {
			if !matches(h.Events, event) {
				continue
			}
			d.deliver(ctx, h, event, payload)
		}
	}()
}

func matches(subscribed, event string) bool {
	for _, s := range strings.Split(subscribed, ",") {
		s = strings.TrimSpace(s)
		if s == "*" || s == event {
			return true
		}
		if strings.HasSuffix(s, ".*") && strings.HasPrefix(event, strings.TrimSuffix(s, "*")) {
			return true
		}
	}
	return false
}

func (d *Dispatcher) deliver(ctx context.Context, h store.Webhook, event string, payload []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(payload))
	if err != nil {
		d.log.Error("webhook: build request", "url", h.URL, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pocket-invoicing-webhook")
	req.Header.Set("X-Webhook-Event", event)
	if h.Secret != "" {
		mac := hmac.New(sha256.New, []byte(h.Secret))
		mac.Write(payload)
		req.Header.Set("X-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		d.log.Warn("webhook: delivery failed", "url", h.URL, "event", event, "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		d.log.Warn("webhook: non-2xx", "url", h.URL, "event", event, "status", resp.StatusCode)
		return
	}
	d.log.Debug("webhook: delivered", "url", h.URL, "event", event)
}
