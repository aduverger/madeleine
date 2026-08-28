package madeleine

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
)

type testService struct {
	*Service
	db *sql.DB
}

func openTestStore(t *testing.T, home string) *testService {
	t.Helper()
	service, err := Open(context.Background(), Options{Home: home})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	dsn := url.URL{Scheme: "file", Path: filepath.Join(home, "madeleine.db")}
	query := dsn.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Set("_txlock", "immediate")
	dsn.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		_ = service.Close()
		t.Fatalf("open test database: %v", err)
	}
	return &testService{Service: service, db: database}
}

func (s *testService) Close() error {
	return errors.Join(s.db.Close(), s.Service.Close())
}

func newTestGitRepository(t *testing.T, origin string) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init")
	if origin != "" {
		git(t, root, "remote", "add", "origin", origin)
	}
	return root
}

func utcTimestamp() string {
	return nowUTC().Format("2006-01-02T15:04:05.999999999Z07:00")
}
