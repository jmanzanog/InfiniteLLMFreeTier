package metrics

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var (
	openDB         = sql.Open
	sqliteDriver   = "sqlite"
	migrateStoreFn = func(store *sqliteStore) error { return store.migrate() }
)

// RequestRecord represents a single request record in the database
type RequestRecord struct {
	ID           int64     `json:"id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	StatusCode   int       `json:"status_code"`
	ResponseTime int64     `json:"response_time_ms"`
	Success      bool      `json:"success"`
	ErrorType    string    `json:"error_type,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ProviderStats represents aggregated statistics for a provider
type ProviderStats struct {
	Provider        string  `json:"provider"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessCount    int64   `json:"success_count"`
	FailureCount    int64   `json:"failure_count"`
	SuccessRate     float64 `json:"success_rate_percent"`
	AvgResponseMs   float64 `json:"avg_response_ms"`
	MinResponseMs   int64   `json:"min_response_ms"`
	MaxResponseMs   int64   `json:"max_response_ms"`
	Error429Count   int64   `json:"error_429_count"`
	Error500Count   int64   `json:"error_5xx_count"`
	Error400Count   int64   `json:"error_4xx_count"`
	ErrorOtherCount int64   `json:"error_other_count"`
	LastRequestAt   string  `json:"last_request_at,omitempty"`
}

// GlobalStats represents overall gateway statistics
type GlobalStats struct {
	TotalRequests      int64           `json:"total_requests"`
	TotalSuccess       int64           `json:"total_success"`
	TotalFailures      int64           `json:"total_failures"`
	OverallSuccessRate float64         `json:"overall_success_rate_percent"`
	AvgResponseMs      float64         `json:"avg_response_ms"`
	MinResponseMs      int64           `json:"min_response_ms"`
	MaxResponseMs      int64           `json:"max_response_ms"`
	ProviderStats      []ProviderStats `json:"providers"`
	Since              string          `json:"stats_since"`
	GeneratedAt        string          `json:"generated_at"`
}

// Store defines the interface for metrics storage
type Store interface {
	SaveRequest(record RequestRecord) error
	SaveRequests(records []RequestRecord) error
	GetGlobalStats() (*GlobalStats, error)
	PurgeOldMetrics(days int) error
	Close() error
}

// sqliteStore handles SQLite persistence for metrics
type sqliteStore struct {
	db *sql.DB
}

// NewStore creates a new SQLite store and returns it as a Store interface
func NewStore(dbPath string) (Store, error) {
	// Enable WAL mode and busy timeout for better concurrency
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := openDB(sqliteDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &sqliteStore{db: db}
	if err := migrateStoreFn(store); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

// migrate creates the necessary tables
func (s *sqliteStore) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		response_time_ms INTEGER NOT NULL,
		success INTEGER NOT NULL,
		error_type TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests(provider);
	CREATE INDEX IF NOT EXISTS idx_requests_created_at ON requests(created_at);
	CREATE INDEX IF NOT EXISTS idx_requests_success ON requests(success);
	`
	_, err := s.db.Exec(query)
	return err
}

// SaveRequest persists a single request record
func (s *sqliteStore) SaveRequest(record RequestRecord) error {
	return s.SaveRequests([]RequestRecord{record})
}

// SaveRequests persists multiple request records in a single transaction
func (s *sqliteStore) SaveRequests(records []RequestRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO requests (provider, model, status_code, response_time_ms, success, error_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, record := range records {
		_, err := stmt.Exec(
			record.Provider,
			record.Model,
			record.StatusCode,
			record.ResponseTime,
			record.Success,
			record.ErrorType,
			record.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to execute statement: %w", err)
		}
	}

	return tx.Commit()
}

// GetGlobalStats returns aggregated statistics
func (s *sqliteStore) GetGlobalStats() (*GlobalStats, error) {
	stats := &GlobalStats{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Global metrics
	globalQuery := `
	SELECT 
		COUNT(*) as total,
		COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0) as success_count,
		COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0) as failure_count,
		COALESCE(AVG(response_time_ms), 0) as avg_response,
		COALESCE(MIN(response_time_ms), 0) as min_response,
		COALESCE(MAX(response_time_ms), 0) as max_response,
		MIN(created_at) as first_request
	FROM requests
	`
	var firstRequest sql.NullString
	err := s.db.QueryRow(globalQuery).Scan(
		&stats.TotalRequests,
		&stats.TotalSuccess,
		&stats.TotalFailures,
		&stats.AvgResponseMs,
		&stats.MinResponseMs,
		&stats.MaxResponseMs,
		&firstRequest,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get global stats: %w", err)
	}

	if stats.TotalRequests > 0 {
		stats.OverallSuccessRate = float64(stats.TotalSuccess) / float64(stats.TotalRequests) * 100
	}
	if firstRequest.Valid {
		stats.Since = firstRequest.String
	}

	// Per-provider metrics
	providerQuery := `
	SELECT 
		provider,
		COUNT(*) as total,
		SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
		SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as failure_count,
		COALESCE(AVG(response_time_ms), 0) as avg_response,
		COALESCE(MIN(response_time_ms), 0) as min_response,
		COALESCE(MAX(response_time_ms), 0) as max_response,
		SUM(CASE WHEN status_code = 429 THEN 1 ELSE 0 END) as error_429,
		SUM(CASE WHEN status_code >= 500 AND status_code < 600 THEN 1 ELSE 0 END) as error_5xx,
		SUM(CASE WHEN status_code >= 400 AND status_code < 500 AND status_code != 429 THEN 1 ELSE 0 END) as error_4xx,
		SUM(CASE WHEN success = 0 AND (status_code < 400 OR status_code >= 600) THEN 1 ELSE 0 END) as error_other,
		MAX(created_at) as last_request
	FROM requests
	GROUP BY provider
	ORDER BY total DESC
	`
	rows, err := s.db.Query(providerQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var ps ProviderStats
		var lastRequest sql.NullString
		err := rows.Scan(
			&ps.Provider,
			&ps.TotalRequests,
			&ps.SuccessCount,
			&ps.FailureCount,
			&ps.AvgResponseMs,
			&ps.MinResponseMs,
			&ps.MaxResponseMs,
			&ps.Error429Count,
			&ps.Error500Count,
			&ps.Error400Count,
			&ps.ErrorOtherCount,
			&lastRequest,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan provider row: %w", err)
		}
		if ps.TotalRequests > 0 {
			ps.SuccessRate = float64(ps.SuccessCount) / float64(ps.TotalRequests) * 100
		}
		if lastRequest.Valid {
			ps.LastRequestAt = lastRequest.String
		}
		stats.ProviderStats = append(stats.ProviderStats, ps)
	}

	return stats, nil
}

// PurgeOldMetrics deletes metric records older than the specified number of days
func (s *sqliteStore) PurgeOldMetrics(days int) error {
	if days <= 0 {
		return nil
	}
	query := `DELETE FROM requests WHERE created_at < datetime('now', '-' || ? || ' days')`
	_, err := s.db.Exec(query, days)
	return err
}

// Close closes the database connection
func (s *sqliteStore) Close() error {
	return s.db.Close()
}
