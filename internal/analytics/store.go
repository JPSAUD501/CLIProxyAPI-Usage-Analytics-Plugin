package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Event struct {
	RequestedAt                                                                                                 time.Time
	Provider, Executor, Model, Alias, APIKey, AuthID, AuthIndex, AuthType, Source, ReasoningEffort, ServiceTier string
	Generate                                                                                                    bool
	LatencyMS, TTFTMS                                                                                           int64
	Failed                                                                                                      bool
	FailureStatus                                                                                               int
	Tokens                                                                                                      TokenBreakdown
	CostNanoUSD, CacheSavingsNanoUSD                                                                            *int64
	PriceVersion                                                                                                string
}

type Store struct {
	db   *sql.DB
	salt []byte
}

func Open(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	_ = os.Chmod(dataDir, 0o700)
	saltPath := filepath.Join(dataDir, "identity-hmac.key")
	salt, err := loadOrCreateSecret(saltPath)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "usage.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	_ = os.Chmod(dbPath, 0o600)
	db.SetMaxOpenConns(8)
	store := &Store{db: db, salt: salt}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func loadOrCreateSecret(path string) ([]byte, error) {
	if existing, err := os.ReadFile(path); err == nil {
		if len(existing) != 32 {
			return nil, errors.New("invalid identity HMAC key length")
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(secret); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	return secret, nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS usage_events(
		 id INTEGER PRIMARY KEY AUTOINCREMENT, requested_at TEXT NOT NULL, provider TEXT NOT NULL, executor TEXT NOT NULL,
		 model TEXT NOT NULL, alias TEXT NOT NULL, client_hmac TEXT NOT NULL, auth_id TEXT NOT NULL, auth_index TEXT NOT NULL,
		 auth_type TEXT NOT NULL, source TEXT NOT NULL, reasoning_effort TEXT NOT NULL, service_tier TEXT NOT NULL,
		 generate INTEGER NOT NULL, latency_ms INTEGER NOT NULL, ttft_ms INTEGER NOT NULL, failed INTEGER NOT NULL,
		 failure_status INTEGER NOT NULL, accounting_quality TEXT NOT NULL, total_tokens INTEGER NOT NULL,
		 input_uncached INTEGER NOT NULL, cache_read INTEGER NOT NULL, cache_creation INTEGER NOT NULL,
		 output_non_reasoning INTEGER NOT NULL, reasoning INTEGER NOT NULL, unclassified INTEGER NOT NULL,
		 cost_nano_usd INTEGER, cache_savings_nano_usd INTEGER, price_version TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS usage_events_requested_at ON usage_events(requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS usage_events_provider_model_time ON usage_events(provider, model, requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS usage_events_auth_time ON usage_events(auth_id, requested_at DESC)`,
		`CREATE TABLE IF NOT EXISTS identity_labels(kind TEXT NOT NULL, identity TEXT NOT NULL, label TEXT NOT NULL, PRIMARY KEY(kind,identity))`,
		`CREATE TABLE IF NOT EXISTS pricing_state(id INTEGER PRIMARY KEY CHECK(id=1), source TEXT NOT NULL, version TEXT NOT NULL, updated_at TEXT NOT NULL, payload BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS pricing_catalogs(catalog_key TEXT PRIMARY KEY, source TEXT NOT NULL, version TEXT NOT NULL, updated_at TEXT NOT NULL, payload BLOB NOT NULL)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'))`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, datetime('now'))`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ClientHMAC(value string) string {
	if value == "" {
		return ""
	}
	h := hmac.New(sha256.New, s.salt)
	_, _ = h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) Insert(ctx context.Context, event Event) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_events(
	 requested_at,provider,executor,model,alias,client_hmac,auth_id,auth_index,auth_type,source,reasoning_effort,service_tier,
	 generate,latency_ms,ttft_ms,failed,failure_status,accounting_quality,total_tokens,input_uncached,cache_read,cache_creation,
	 output_non_reasoning,reasoning,unclassified,cost_nano_usd,cache_savings_nano_usd,price_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.RequestedAt.UTC().Format(time.RFC3339Nano), event.Provider, event.Executor, event.Model, event.Alias, s.ClientHMAC(event.APIKey),
		event.AuthID, event.AuthIndex, event.AuthType, event.Source, event.ReasoningEffort, event.ServiceTier, event.Generate,
		event.LatencyMS, event.TTFTMS, event.Failed, event.FailureStatus, event.Tokens.Quality, event.Tokens.Total,
		event.Tokens.InputUncached, event.Tokens.CacheRead, event.Tokens.CacheCreation, event.Tokens.OutputNonReasoning, event.Tokens.Reasoning, event.Tokens.Unclassified, event.CostNanoUSD, event.CacheSavingsNanoUSD, event.PriceVersion)
	return err
}

type Filters struct {
	From, To                                                  time.Time
	Provider, Model, ReasoningEffort, Source, Account, Client string
	Limit, Offset                                             int
}

func filterClause(f Filters) (string, []any) {
	clauses := []string{"requested_at >= ?", "requested_at < ?"}
	args := []any{f.From.UTC().Format(time.RFC3339Nano), f.To.UTC().Format(time.RFC3339Nano)}
	for _, item := range []struct{ column, value string }{{"provider", f.Provider}, {"model", f.Model}, {"reasoning_effort", f.ReasoningEffort}, {"source", f.Source}, {"auth_id", f.Account}, {"client_hmac", f.Client}} {
		if item.value != "" {
			clauses = append(clauses, item.column+" = ?")
			args = append(args, item.value)
		}
	}
	return strings.Join(clauses, " AND "), args
}

type Summary struct {
	Requests, Failed, Total, InputUncached, CacheRead, CacheCreation, OutputNonReasoning, Reasoning, Unclassified int64
	LatencyP50MS, LatencyP95MS, TTFTP50MS, TTFTP95MS                                                              int64
	CostNanoUSD, CacheSavingsNanoUSD                                                                              int64
	UnpricedEvents                                                                                                int64
}

func (s *Store) Summary(ctx context.Context, f Filters) (Summary, error) {
	where, args := filterClause(f)
	var out Summary
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(failed),0),COALESCE(SUM(total_tokens),0),COALESCE(SUM(input_uncached),0),COALESCE(SUM(cache_read),0),COALESCE(SUM(cache_creation),0),COALESCE(SUM(output_non_reasoning),0),COALESCE(SUM(reasoning),0),COALESCE(SUM(unclassified),0),COALESCE(SUM(cost_nano_usd),0),COALESCE(SUM(cache_savings_nano_usd),0),COALESCE(SUM(CASE WHEN cost_nano_usd IS NULL THEN 1 ELSE 0 END),0) FROM usage_events WHERE `+where, args...)
	err := row.Scan(&out.Requests, &out.Failed, &out.Total, &out.InputUncached, &out.CacheRead, &out.CacheCreation, &out.OutputNonReasoning, &out.Reasoning, &out.Unclassified, &out.CostNanoUSD, &out.CacheSavingsNanoUSD, &out.UnpricedEvents)
	if err != nil {
		return out, err
	}
	out.LatencyP50MS, _ = s.percentile(ctx, "latency_ms", 0.50, where, args)
	out.LatencyP95MS, _ = s.percentile(ctx, "latency_ms", 0.95, where, args)
	out.TTFTP50MS, _ = s.percentile(ctx, "ttft_ms", 0.50, where+" AND ttft_ms > 0", args)
	out.TTFTP95MS, _ = s.percentile(ctx, "ttft_ms", 0.95, where+" AND ttft_ms > 0", args)
	return out, nil
}

func (s *Store) percentile(ctx context.Context, column string, p float64, where string, args []any) (int64, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_events WHERE "+where, args...).Scan(&count); err != nil || count == 0 {
		return 0, err
	}
	offset := int(float64(count-1) * p)
	var value int64
	err := s.db.QueryRowContext(ctx, "SELECT "+column+" FROM usage_events WHERE "+where+" ORDER BY "+column+" LIMIT 1 OFFSET ?", append(args, offset)...).Scan(&value)
	return value, err
}

type BreakdownRow struct {
	Key                                   string `json:"key"`
	Requests, Failed, Tokens, CostNanoUSD int64
}

type SeriesRow struct {
	Bucket, Provider              string
	Requests, Tokens, CostNanoUSD int64
}

func (s *Store) Series(ctx context.Context, f Filters, bucket string, timezoneOffset int) ([]SeriesRow, error) {
	if bucket != "hour" && bucket != "day" {
		return nil, errors.New("invalid bucket")
	}
	if timezoneOffset < -840 || timezoneOffset > 840 {
		return nil, errors.New("invalid timezone offset")
	}
	where, args := filterClause(f)
	modifier := fmt.Sprintf("%+d minutes", -timezoneOffset)
	format := "%Y-%m-%d"
	if bucket == "hour" {
		format = "%Y-%m-%dT%H:00:00"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT strftime(?,requested_at,?),provider,COUNT(*),COALESCE(SUM(total_tokens),0),COALESCE(SUM(cost_nano_usd),0) FROM usage_events WHERE `+where+` GROUP BY 1,2 ORDER BY 1,2`, append([]any{format, modifier}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeriesRow{}
	for rows.Next() {
		var row SeriesRow
		if err := rows.Scan(&row.Bucket, &row.Provider, &row.Requests, &row.Tokens, &row.CostNanoUSD); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) Breakdown(ctx context.Context, f Filters, dimension string) ([]BreakdownRow, error) {
	columns := map[string]string{"provider": "provider", "model": "model", "account": "auth_id", "client": "client_hmac", "source": "source", "reasoning_effort": "reasoning_effort"}
	column, ok := columns[dimension]
	if !ok {
		return nil, errors.New("invalid dimension")
	}
	where, args := filterClause(f)
	rows, err := s.db.QueryContext(ctx, "SELECT "+column+",COUNT(*),COALESCE(SUM(failed),0),COALESCE(SUM(total_tokens),0),COALESCE(SUM(cost_nano_usd),0) FROM usage_events WHERE "+where+" GROUP BY "+column+" ORDER BY SUM(total_tokens) DESC LIMIT 200", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BreakdownRow{}
	for rows.Next() {
		var item BreakdownRow
		if err := rows.Scan(&item.Key, &item.Requests, &item.Failed, &item.Tokens, &item.CostNanoUSD); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type RequestRow struct {
	ID                                                                                                             int64 `json:"id"`
	RequestedAt, Provider, Model, Source, ReasoningEffort, AuthID, ClientHMAC, Quality                             string
	Failed                                                                                                         bool
	FailureStatus                                                                                                  int
	LatencyMS, TTFTMS, Total, InputUncached, CacheRead, CacheCreation, OutputNonReasoning, Reasoning, Unclassified int64
}

func (s *Store) Requests(ctx context.Context, f Filters) ([]RequestRow, error) {
	where, args := filterClause(f)
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args = append(args, limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,requested_at,provider,model,source,reasoning_effort,auth_id,client_hmac,accounting_quality,failed,failure_status,latency_ms,ttft_ms,total_tokens,input_uncached,cache_read,cache_creation,output_non_reasoning,reasoning,unclassified FROM usage_events WHERE `+where+` ORDER BY requested_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RequestRow{}
	for rows.Next() {
		var x RequestRow
		if err := rows.Scan(&x.ID, &x.RequestedAt, &x.Provider, &x.Model, &x.Source, &x.ReasoningEffort, &x.AuthID, &x.ClientHMAC, &x.Quality, &x.Failed, &x.FailureStatus, &x.LatencyMS, &x.TTFTMS, &x.Total, &x.InputUncached, &x.CacheRead, &x.CacheCreation, &x.OutputNonReasoning, &x.Reasoning, &x.Unclassified); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM usage_events WHERE requested_at < ?", before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type Identity struct {
	Kind, ID, Label string
	Requests        int64
}

func (s *Store) Identities(ctx context.Context) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,identity,COALESCE(label,''),requests FROM (
 SELECT 'account' kind,auth_id identity,COUNT(*) requests FROM usage_events WHERE auth_id<>'' GROUP BY auth_id
 UNION ALL SELECT 'client',client_hmac,COUNT(*) FROM usage_events WHERE client_hmac<>'' GROUP BY client_hmac
) x LEFT JOIN identity_labels l ON l.kind=x.kind AND l.identity=x.identity ORDER BY kind,requests DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var x Identity
		if err := rows.Scan(&x.Kind, &x.ID, &x.Label, &x.Requests); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) SetLabel(ctx context.Context, kind, id, label string) error {
	if kind != "account" && kind != "client" {
		return errors.New("invalid identity kind")
	}
	if id == "" || len(label) > 80 {
		return errors.New("invalid label")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO identity_labels(kind,identity,label) VALUES(?,?,?) ON CONFLICT(kind,identity) DO UPDATE SET label=excluded.label`, kind, id, strings.TrimSpace(label))
	return err
}
func (s *Store) SavePricing(ctx context.Context, source, version string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pricing_state(id,source,version,updated_at,payload) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET source=excluded.source,version=excluded.version,updated_at=excluded.updated_at,payload=excluded.payload`, source, version, time.Now().UTC().Format(time.RFC3339Nano), payload)
	return err
}
func (s *Store) SavePricingCatalog(ctx context.Context, key, source, version string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pricing_catalogs(catalog_key,source,version,updated_at,payload) VALUES(?,?,?,?,?) ON CONFLICT(catalog_key) DO UPDATE SET source=excluded.source,version=excluded.version,updated_at=excluded.updated_at,payload=excluded.payload`, key, source, version, time.Now().UTC().Format(time.RFC3339Nano), payload)
	return err
}
func (s *Store) LoadPricingCatalog(ctx context.Context, key string) (string, string, time.Time, []byte, error) {
	var source, version, updated string
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT source,version,updated_at,payload FROM pricing_catalogs WHERE catalog_key=?`, key).Scan(&source, &version, &updated, &payload)
	if err != nil {
		return "", "", time.Time{}, nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, updated)
	return source, version, at, payload, err
}
func (s *Store) RepriceUnpriced(ctx context.Context, pricing Pricing, provider string) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider,model,input_uncached,cache_read,cache_creation,output_non_reasoning,reasoning FROM usage_events WHERE cost_nano_usd IS NULL AND (?='' OR provider=?)`, provider, provider)
	if err != nil {
		return 0, err
	}
	type update struct{ id, cost, savings int64 }
	updates := []update{}
	for rows.Next() {
		var id int64
		var eventProvider, model string
		var b TokenBreakdown
		if err := rows.Scan(&id, &eventProvider, &model, &b.InputUncached, &b.CacheRead, &b.CacheCreation, &b.OutputNonReasoning, &b.Reasoning); err != nil {
			rows.Close()
			return 0, err
		}
		if cost, savings, ok := pricing.Cost(eventProvider, model, b); ok {
			updates = append(updates, update{id, cost, savings})
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE usage_events SET cost_nano_usd=?,cache_savings_nano_usd=?,price_version=? WHERE id=? AND cost_nano_usd IS NULL`, item.cost, item.savings, pricing.Source+":"+pricing.Version, item.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(updates)), nil
}
func (s *Store) LoadPricing(ctx context.Context) (string, string, time.Time, []byte, error) {
	var source, version, updated string
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT source,version,updated_at,payload FROM pricing_state WHERE id=1`).Scan(&source, &version, &updated, &payload)
	if err != nil {
		return "", "", time.Time{}, nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, updated)
	return source, version, at, payload, err
}
func (s *Store) Health(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                     { return s.db.Close() }
