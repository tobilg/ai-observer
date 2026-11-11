package storage

import (
	"context"
	"fmt"
)

// Ping checks that the database answers a query (for readiness checks)
func (s *DuckDBStore) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("database check: %w", err)
	}
	return nil
}
