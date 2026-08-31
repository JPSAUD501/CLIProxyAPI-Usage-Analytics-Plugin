package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Enabled       bool   `yaml:"enabled"`
	DataDir       string `yaml:"data_dir"`
	RetentionDays int    `yaml:"retention_days"`
	IdentityMode  string `yaml:"identity_mode"`
}
type Service struct {
	mu                sync.RWMutex
	refreshMu         sync.Mutex
	config            Config
	store             *Store
	queue             chan Event
	done              chan struct{}
	dropped           atomic.Int64
	once              sync.Once
	call              HostCaller
	pricing           Pricing
	openRouterPricing Pricing
}

type HostCaller func(method string, payload any) (json.RawMessage, error)

func New(call HostCaller) *Service {
	return &Service{queue: make(chan Event, 4096), done: make(chan struct{}), call: call}
}

func (s *Service) Configure(raw []byte) error {
	cfg := Config{Enabled: true, RetentionDays: 90, IdentityMode: "full"}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return err
		}
		if cfg.RetentionDays == 0 {
			cfg.RetentionDays = 90
		}
		if cfg.IdentityMode == "" {
			cfg.IdentityMode = "full"
		}
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > 3650 {
		return errors.New("retention-days must be between 1 and 3650")
	}
	if cfg.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		cfg.DataDir = filepath.Join(home, ".cli-proxy-api", "plugins", "usage-analytics")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		store, err := Open(cfg.DataDir)
		if err != nil {
			return err
		}
		s.store = store
		if source, version, updated, payload, err := store.LoadPricing(context.Background()); err == nil {
			if pricing, err := ParsePricing(payload, source, version); err == nil {
				pricing.UpdatedAt = updated
				s.pricing = pricing
			}
		}
		if len(s.pricing.Models) == 0 {
			if pricing, err := ParsePricing(fallbackPrices, "bundled LiteLLM fallback", "2026-08-30"); err == nil {
				s.pricing = pricing
			}
		}
		if source, version, updated, payload, err := store.LoadPricingCatalog(context.Background(), "openrouter"); err == nil {
			if pricing, err := ParseOpenRouterPricing(payload, version); err == nil {
				pricing.Source = source
				pricing.UpdatedAt = updated
				s.openRouterPricing = pricing
			}
		}
		s.once.Do(func() { go s.writer() })
	}
	s.config = cfg
	return nil
}

func (s *Service) writer() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case event := <-s.queue:
			s.mu.RLock()
			store := s.store
			cfg := s.config
			s.mu.RUnlock()
			if store != nil && cfg.Enabled {
				s.mu.RLock()
				pricing := s.pricing
				openRouterPricing := s.openRouterPricing
				s.mu.RUnlock()
				if strings.EqualFold(event.Provider, "openrouter") && len(openRouterPricing.Models) > 0 {
					pricing = openRouterPricing
				}
				if cost, savings, ok := pricing.Cost(event.Provider, event.Model, event.Tokens); ok {
					event.CostNanoUSD = &cost
					event.CacheSavingsNanoUSD = &savings
					event.PriceVersion = pricing.Version
				}
				_ = store.Insert(context.Background(), event)
			}
		case <-ticker.C:
			s.mu.RLock()
			store := s.store
			days := s.config.RetentionDays
			s.mu.RUnlock()
			if store != nil {
				_, _ = store.Purge(context.Background(), time.Now().AddDate(0, 0, -days))
			}
		case <-s.done:
			return
		}
	}
}

type priceHTTPRequest struct {
	HostCallbackID string `json:"host_callback_id,omitempty"`
	Method, URL    string
	Headers        http.Header
	Body           []byte
}
type priceHTTPResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       []byte      `json:"body"`
}

func (r *priceHTTPResponse) UnmarshalJSON(raw []byte) error {
	var wire struct {
		CoreStatusCode      int         `json:"StatusCode"`
		CanonicalStatusCode int         `json:"status_code"`
		CoreHeaders         http.Header `json:"Headers"`
		CanonicalHeaders    http.Header `json:"headers"`
		CoreBody            []byte      `json:"Body"`
		CanonicalBody       []byte      `json:"body"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	r.StatusCode = wire.CoreStatusCode
	if r.StatusCode == 0 {
		r.StatusCode = wire.CanonicalStatusCode
	}
	r.Headers = wire.CoreHeaders
	if r.Headers == nil {
		r.Headers = wire.CanonicalHeaders
	}
	r.Body = wire.CoreBody
	if r.Body == nil {
		r.Body = wire.CanonicalBody
	}
	return nil
}
func (s *Service) refreshPricing(hostCallbackID string) {
	if s.call == nil {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.refreshCatalog("litellm", priceCatalogURL, hostCallbackID)
	s.refreshCatalog("openrouter", openRouterCatalogURL, hostCallbackID)
}
func (s *Service) refreshCatalog(key, catalogURL, hostCallbackID string) {
	raw, err := s.call("host.http.do", priceHTTPRequest{HostCallbackID: hostCallbackID, Method: http.MethodGet, URL: catalogURL, Headers: http.Header{"Accept": {"application/json"}, "User-Agent": {"CLIProxyAPI-Usage-Analytics/1.1.1"}}})
	if err != nil {
		return
	}
	var resp priceHTTPResponse
	if json.Unmarshal(raw, &resp) != nil || resp.StatusCode != http.StatusOK {
		return
	}
	version := priceVersion(resp.Headers)
	var pricing Pricing
	if key == "openrouter" {
		pricing, err = ParseOpenRouterPricing(resp.Body, version)
	} else {
		pricing, err = ParsePricing(resp.Body, "LiteLLM", version)
	}
	if err != nil {
		return
	}
	s.mu.Lock()
	store := s.store
	if key == "openrouter" {
		s.openRouterPricing = pricing
	} else {
		s.pricing = pricing
	}
	s.mu.Unlock()
	if store != nil {
		if key == "openrouter" {
			_ = store.SavePricingCatalog(context.Background(), key, pricing.Source, pricing.Version, resp.Body)
			_, _ = store.RepriceUnpriced(context.Background(), pricing, "openrouter")
		} else {
			_ = store.SavePricing(context.Background(), pricing.Source, pricing.Version, resp.Body)
			_, _ = store.RepriceUnpriced(context.Background(), pricing, "")
		}
	}
}

func (s *Service) HandleUsage(record pluginapi.UsageRecord) {
	s.mu.RLock()
	enabled := s.config.Enabled
	s.mu.RUnlock()
	if !enabled {
		return
	}
	event := Event{RequestedAt: record.RequestedAt, Provider: record.Provider, Executor: record.ExecutorType, Model: record.Model, Alias: record.Alias, APIKey: record.APIKey, AuthID: record.AuthID, AuthIndex: record.AuthIndex, AuthType: record.AuthType, Source: record.Source, ReasoningEffort: record.ReasoningEffort, ServiceTier: record.ServiceTier, Generate: record.Generate, LatencyMS: record.Latency.Milliseconds(), TTFTMS: record.TTFT.Milliseconds(), Failed: record.Failed, FailureStatus: record.Failure.StatusCode, Tokens: Account(record.Provider, record.ExecutorType, RawUsage{Input: record.Detail.InputTokens, Output: record.Detail.OutputTokens, Reasoning: record.Detail.ReasoningTokens, Cached: record.Detail.CachedTokens, CacheRead: record.Detail.CacheReadTokens, CacheCreation: record.Detail.CacheCreationTokens, Total: record.Detail.TotalTokens})}
	if event.RequestedAt.IsZero() {
		event.RequestedAt = time.Now()
	}
	select {
	case s.queue <- event:
	default:
		s.dropped.Add(1)
	}
}

func (s *Service) Registration() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{Routes: []pluginapi.ManagementRoute{
		{Method: http.MethodGet, Path: "/plugins/usage-analytics/summary"}, {Method: http.MethodGet, Path: "/plugins/usage-analytics/breakdown"}, {Method: http.MethodGet, Path: "/plugins/usage-analytics/series"}, {Method: http.MethodGet, Path: "/plugins/usage-analytics/requests"}, {Method: http.MethodGet, Path: "/plugins/usage-analytics/identities"}, {Method: http.MethodPatch, Path: "/plugins/usage-analytics/labels"}, {Method: http.MethodPost, Path: "/plugins/usage-analytics/maintenance/purge"}, {Method: http.MethodGet, Path: "/plugins/usage-analytics/health"},
	}, Resources: []pluginapi.ResourceRoute{{Path: "/dashboard", Menu: "Usage analytics", Description: "Tokens, cache, cost and reliability analytics."}, {Path: "/dashboard.js"}}}
}

func (s *Service) Management(req pluginapi.ManagementRequest, hostCallbackID string) (pluginapi.ManagementResponse, error) {
	headers := http.Header{"Content-Type": {"application/json; charset=utf-8"}, "Cache-Control": {"no-store"}, "Content-Security-Policy": {"default-src 'none'; frame-ancestors 'none'"}}
	if strings.HasSuffix(req.Path, "/dashboard.js") {
		return pluginapi.ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/javascript; charset=utf-8"}, "Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}}, Body: DashboardJS()}, nil
	}
	if strings.HasSuffix(req.Path, "/dashboard") {
		return pluginapi.ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "Content-Security-Policy": {"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'self'"}}, Body: DashboardHTML()}, nil
	}
	if strings.HasSuffix(req.Path, "/summary") {
		s.mu.RLock()
		liteUpdated, openRouterUpdated := s.pricing.UpdatedAt, s.openRouterPricing.UpdatedAt
		s.mu.RUnlock()
		if liteUpdated.IsZero() || openRouterUpdated.IsZero() || time.Since(liteUpdated) > 24*time.Hour || time.Since(openRouterUpdated) > 24*time.Hour {
			s.refreshPricing(hostCallbackID)
		}
	}
	s.mu.RLock()
	store := s.store
	cfg := s.config
	pricing := s.pricing
	openRouterPricing := s.openRouterPricing
	s.mu.RUnlock()
	if store == nil {
		return jsonResponse(headers, 503, map[string]any{"error": "store_unavailable"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	filters, err := parseFilters(req.Query)
	if err != nil {
		return jsonResponse(headers, 400, map[string]any{"error": err.Error()})
	}
	switch {
	case strings.HasSuffix(req.Path, "/summary"):
		data, err := store.Summary(ctx, filters)
		if err != nil {
			return pluginapi.ManagementResponse{}, err
		}
		return jsonResponse(headers, 200, map[string]any{"data": data, "dropped_events": s.dropped.Load(), "retention_days": cfg.RetentionDays, "pricing": []map[string]any{{"source": pricing.Source, "version": pricing.Version, "updated_at": pricing.UpdatedAt}, {"source": openRouterPricing.Source, "version": openRouterPricing.Version, "updated_at": openRouterPricing.UpdatedAt}}})
	case strings.HasSuffix(req.Path, "/breakdown"):
		data, err := store.Breakdown(ctx, filters, req.Query.Get("dimension"))
		if err != nil {
			return jsonResponse(headers, 400, map[string]any{"error": err.Error()})
		}
		return jsonResponse(headers, 200, map[string]any{"data": data})
	case strings.HasSuffix(req.Path, "/series"):
		offset, _ := strconv.Atoi(req.Query.Get("timezone_offset"))
		data, err := store.Series(ctx, filters, req.Query.Get("bucket"), offset)
		if err != nil {
			return jsonResponse(headers, 400, map[string]any{"error": err.Error()})
		}
		return jsonResponse(headers, 200, map[string]any{"data": data})
	case strings.HasSuffix(req.Path, "/requests"):
		data, err := store.Requests(ctx, filters)
		if err != nil {
			return pluginapi.ManagementResponse{}, err
		}
		return jsonResponse(headers, 200, map[string]any{"data": data})
	case strings.HasSuffix(req.Path, "/identities"):
		data, err := store.Identities(ctx)
		if err != nil {
			return pluginapi.ManagementResponse{}, err
		}
		return jsonResponse(headers, 200, map[string]any{"data": data})
	case strings.HasSuffix(req.Path, "/labels"):
		var body struct{ Kind, ID, Label string }
		if len(req.Body) > 4096 || json.Unmarshal(req.Body, &body) != nil {
			return jsonResponse(headers, 400, map[string]any{"error": "invalid_body"})
		}
		if err := store.SetLabel(ctx, body.Kind, body.ID, body.Label); err != nil {
			return jsonResponse(headers, 400, map[string]any{"error": err.Error()})
		}
		return jsonResponse(headers, 200, map[string]any{"ok": true})
	case strings.HasSuffix(req.Path, "/maintenance/purge"):
		var body struct {
			Before string `json:"before"`
		}
		if len(req.Body) > 4096 || json.Unmarshal(req.Body, &body) != nil {
			return jsonResponse(headers, 400, map[string]any{"error": "invalid_body"})
		}
		before, err := time.Parse(time.RFC3339, body.Before)
		if err != nil {
			return jsonResponse(headers, 400, map[string]any{"error": "invalid_before"})
		}
		count, err := store.Purge(ctx, before)
		if err != nil {
			return pluginapi.ManagementResponse{}, err
		}
		return jsonResponse(headers, 200, map[string]any{"purged": count})
	case strings.HasSuffix(req.Path, "/health"):
		if err := store.Health(ctx); err != nil {
			return jsonResponse(headers, 503, map[string]any{"status": "unhealthy"})
		}
		return jsonResponse(headers, 200, map[string]any{"status": "healthy", "queue_depth": len(s.queue), "dropped_events": s.dropped.Load()})
	}
	return jsonResponse(headers, 404, map[string]any{"error": "not_found"})
}

func parseFilters(q url.Values) (Filters, error) {
	now := time.Now()
	f := Filters{To: now, From: now.Add(-24 * time.Hour), Provider: q.Get("provider"), Model: q.Get("model"), ReasoningEffort: q.Get("reasoning_effort"), Source: q.Get("source"), Account: q.Get("account"), Client: q.Get("client")}
	if value := q.Get("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return f, errors.New("invalid from")
		}
		f.From = parsed
	}
	if value := q.Get("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return f, errors.New("invalid to")
		}
		f.To = parsed
	}
	if !f.From.Before(f.To) || f.To.Sub(f.From) > 366*24*time.Hour {
		return f, errors.New("invalid period")
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	return f, nil
}
func jsonResponse(headers http.Header, status int, value any) (pluginapi.ManagementResponse, error) {
	body, err := json.Marshal(value)
	return pluginapi.ManagementResponse{StatusCode: status, Headers: headers, Body: body}, err
}
func (s *Service) Shutdown() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.mu.Lock()
	if s.store != nil {
		_ = s.store.Close()
		s.store = nil
	}
	s.mu.Unlock()
}
