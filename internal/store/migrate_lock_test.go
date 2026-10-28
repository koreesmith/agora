package store

import (
	"sync"
	"testing"
)

// AGORA-399: Migrate took no lock, so two processes migrating one database at
// once could run the same statement together. One then failed with errors like
// `constraint "custom_feed_filters_filter_type_check" ... already exists`,
// which is what made parallel `go test ./...` runs flake. Several concurrent
// migrators must now all succeed.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestMigrateConcurrently(t *testing.T) {
	const dsn = "postgres://agora:agora@localhost:15433/agora_test?sslmode=disable"
	const migrators = 4

	dbs := make([]*DB, migrators)
	for i := range dbs {
		db, err := Open(dsn)
		if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
		t.Cleanup(func() { db.Close() })
		dbs[i] = db
	}

	errs := make([]error, migrators)
	var wg sync.WaitGroup
	for i, db := range dbs {
		wg.Add(1)
		go func(i int, db *DB) {
			defer wg.Done()
			errs[i] = db.Migrate()
		}(i, db)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil { t.Errorf("migrator %d: %v", i, err) }
	}
}
