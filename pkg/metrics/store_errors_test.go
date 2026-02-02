package metrics

import (
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestNewStore_OpenError(t *testing.T) {
	originalOpenDB := openDB
	openDB = func(_ string, _ string) (*sql.DB, error) {
		return nil, errors.New("open fail")
	}
	t.Cleanup(func() { openDB = originalOpenDB })

	_, err := NewStore("bad.db")
	if err == nil {
		t.Fatal("expected open error")
	}
}

func TestNewStore_MigrateError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	originalOpenDB := openDB
	originalMigrate := migrateStoreFn
	openDB = func(_ string, _ string) (*sql.DB, error) { return db, nil }
	migrateStoreFn = func(_ *sqliteStore) error { return errors.New("migrate fail") }
	t.Cleanup(func() {
		openDB = originalOpenDB
		migrateStoreFn = originalMigrate
	})

	_, err = NewStore("test.db")
	if err == nil {
		t.Fatal("expected migrate error")
	}
}

func TestStore_SaveRequests_Empty(t *testing.T) {
	store := &sqliteStore{}
	if err := store.SaveRequests(nil); err != nil {
		t.Fatalf("expected nil error for empty records, got %v", err)
	}
}

func TestStore_SaveRequests_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectBegin().WillReturnError(errors.New("begin fail"))

	if err := store.SaveRequests([]RequestRecord{{Provider: "p", Model: "m"}}); err == nil {
		t.Fatal("expected begin error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_SaveRequests_PrepareError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO requests").WillReturnError(errors.New("prepare fail"))
	mock.ExpectRollback()

	if err := store.SaveRequests([]RequestRecord{{Provider: "p", Model: "m"}}); err == nil {
		t.Fatal("expected prepare error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_SaveRequests_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectBegin()
	prep := mock.ExpectPrepare("INSERT INTO requests")
	prep.ExpectExec().WillReturnError(errors.New("exec fail"))
	mock.ExpectRollback()

	if err := store.SaveRequests([]RequestRecord{{Provider: "p", Model: "m"}}); err == nil {
		t.Fatal("expected exec error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_SaveRequests_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectBegin()
	prep := mock.ExpectPrepare("INSERT INTO requests")
	prep.ExpectExec().WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit fail"))

	if err := store.SaveRequests([]RequestRecord{{Provider: "p", Model: "m"}}); err == nil {
		t.Fatal("expected commit error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_GetGlobalStats_GlobalQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)").WillReturnError(errors.New("query fail"))

	if _, err := store.GetGlobalStats(); err == nil {
		t.Fatal("expected global query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_GetGlobalStats_ProviderQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	globalRows := sqlmock.NewRows([]string{
		"total",
		"success_count",
		"failure_count",
		"avg_response",
		"min_response",
		"max_response",
		"first_request",
	}).AddRow(0, 0, 0, 0, 0, 0, nil)

	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)").WillReturnRows(globalRows)
	mock.ExpectQuery("SELECT\\s+provider").WillReturnError(errors.New("provider query fail"))

	if _, err := store.GetGlobalStats(); err == nil {
		t.Fatal("expected provider query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_GetGlobalStats_ProviderScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	globalRows := sqlmock.NewRows([]string{
		"total",
		"success_count",
		"failure_count",
		"avg_response",
		"min_response",
		"max_response",
		"first_request",
	}).AddRow(1, 1, 0, 10.0, 10, 10, "2025-01-01")

	providerRows := sqlmock.NewRows([]string{
		"provider",
		"total",
		"success_count",
		"failure_count",
		"avg_response",
		"min_response",
		"max_response",
		"error_429",
		"error_5xx",
		"error_4xx",
		"error_other",
		"last_request",
	}).AddRow("p", "bad", 1, 0, 10.0, 10, 10, 0, 0, 0, 0, "2025-01-01")

	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\)").WillReturnRows(globalRows)
	mock.ExpectQuery("SELECT\\s+provider").WillReturnRows(providerRows)

	if _, err := store.GetGlobalStats(); err == nil {
		t.Fatal("expected provider scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStore_PurgeOldMetrics_NoOp(t *testing.T) {
	store := &sqliteStore{}
	if err := store.PurgeOldMetrics(0); err != nil {
		t.Fatalf("expected nil error for no-op purge, got %v", err)
	}
}

func TestStore_PurgeOldMetrics_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := &sqliteStore{db: db}
	mock.ExpectExec("DELETE FROM requests").WillReturnError(errors.New("purge fail"))

	if err := store.PurgeOldMetrics(1); err == nil {
		t.Fatal("expected purge error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
