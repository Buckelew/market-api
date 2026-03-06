package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite"

	"market-api/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type PersistInput struct {
	RequestID           string
	Input               models.ResolveRequestInput
	NormalizedTitle     string
	NormalizedBody      string
	NormalizedImageURL  string
	NormalizedImageURLs []string
	Fingerprint         string
	Classification      models.Classification
	Signals             models.Signals
	Response            models.ResolveResponse
	Status              string
	ProviderResults     []models.ProviderResult
}

func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func ApplyMigrations(ctx context.Context, db *sql.DB, dir string) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		filenames = append(filenames, entry.Name())
	}
	slices.Sort(filenames)

	for _, name := range filenames {
		var applied string
		err := db.QueryRowContext(ctx, `SELECT version FROM schema_migrations WHERE version = ?`, name).Scan(&applied)
		if err == nil {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", name, err)
		}

		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, name, nowUTC()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}

func (s *Store) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) FindCachedResponse(ctx context.Context, fingerprint string) (*models.ResolveResponse, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT rr.response_json
		FROM resolve_results rr
		INNER JOIN resolve_requests req ON req.id = rr.request_id
		WHERE req.fingerprint = ?
		LIMIT 1
	`, fingerprint).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find cached response: %w", err)
	}

	var response models.ResolveResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, fmt.Errorf("decode cached response: %w", err)
	}

	return &response, nil
}

func (s *Store) SaveResolution(ctx context.Context, input PersistInput) error {
	requestJSON, err := json.Marshal(input.Input)
	if err != nil {
		return fmt.Errorf("marshal request json: %w", err)
	}

	classificationReasons, err := json.Marshal(input.Classification.Reasons)
	if err != nil {
		return fmt.Errorf("marshal classification reasons: %w", err)
	}

	signalsJSON, err := json.Marshal(input.Signals)
	if err != nil {
		return fmt.Errorf("marshal signals: %w", err)
	}

	responseJSON, err := json.Marshal(input.Response)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	now := nowUTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save resolution: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resolve_requests (
			id, source, source_listing_id, source_url, title, description, image_url, image_urls_json,
			normalized_title, normalized_description, normalized_image_url, normalized_image_urls_json,
			fingerprint, request_json, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.RequestID, input.Input.Source, input.Input.SourceListingID, input.Input.SourceURL, input.Input.Title, input.Input.Description, input.Input.ImageURL, mustJSONString(input.Input.ImageURLs), input.NormalizedTitle, input.NormalizedBody, input.NormalizedImageURL, mustJSONString(input.NormalizedImageURLs), input.Fingerprint, string(requestJSON), input.Status, now, now); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert resolve_request: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO classification_results (
			request_id, product_type, category, market, confidence, reasons_json, signals_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, input.RequestID, input.Classification.ProductType, input.Classification.Category, input.Classification.Market, input.Classification.Confidence, string(classificationReasons), string(signalsJSON), now); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert classification_result: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resolve_results (id, request_id, response_json, created_at)
		VALUES (?, ?, ?, ?)
	`, input.RequestID, input.RequestID, string(responseJSON), now); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert resolve_result: %w", err)
	}

	for index, providerResult := range input.ProviderResults {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provider_results (
				id, request_id, provider, market, query, raw_payload, normalized_json, confidence, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, fmt.Sprintf("%s:%d", input.RequestID, index+1), input.RequestID, providerResult.Provider, providerResult.Market, providerResult.Query, providerResult.RawPayload, providerResult.NormalizedJSON, providerResult.Confidence, now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert provider_result: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save resolution: %w", err)
	}

	return nil
}

func (s *Store) GetRequest(ctx context.Context, id string) (*models.RequestRecord, error) {
	var record models.RequestRecord
	var imageURLsJSON string
	var normalizedImageURLsJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, source, source_listing_id, source_url, title, description, image_url, image_urls_json,
		       normalized_title, normalized_description, normalized_image_url, normalized_image_urls_json, fingerprint,
		       status, created_at, updated_at, request_json
		FROM resolve_requests
		WHERE id = ?
	`, id).Scan(
		&record.ID,
		&record.Source,
		&record.SourceListingID,
		&record.SourceURL,
		&record.Title,
		&record.Description,
		&record.ImageURL,
		&imageURLsJSON,
		&record.NormalizedTitle,
		&record.NormalizedBody,
		&record.NormalizedImage,
		&normalizedImageURLsJSON,
		&record.Fingerprint,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.OriginalJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}

	if err := json.Unmarshal([]byte(imageURLsJSON), &record.ImageURLs); err != nil {
		return nil, fmt.Errorf("decode request image urls: %w", err)
	}
	if err := json.Unmarshal([]byte(normalizedImageURLsJSON), &record.NormalizedImages); err != nil {
		return nil, fmt.Errorf("decode normalized image urls: %w", err)
	}

	return &record, nil
}

func (s *Store) GetResult(ctx context.Context, id string) (*models.ResolveResponse, error) {
	var raw string

	err := s.db.QueryRowContext(ctx, `
		SELECT response_json
		FROM resolve_results
		WHERE id = ?
	`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get result: %w", err)
	}

	var response models.ResolveResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}

	return &response, nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func mustJSONString(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}
