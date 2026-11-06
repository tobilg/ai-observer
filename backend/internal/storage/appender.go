package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"unsafe"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/ai-observer/internal/redact"
)

// writeTx is a write transaction on a dedicated connection. SQL statements run
// through tx; bulk inserts use DuckDB appenders on the same connection, so both
// commit or roll back together.
type writeTx struct {
	conn *sql.Conn
	tx   *sql.Tx
	// redact replaces secrets in the text of the rows written (may be nil)
	redact *redact.Redactor
	// shared holds the redacted copies of attribute maps that many records
	// share, such as the resource attributes of an export, by map identity
	shared map[unsafe.Pointer]map[string]string
}

// redactShared returns m redacted, redacting each map once per transaction:
// the records of an OTLP export share their resource and scope attribute
// maps, which would otherwise be redacted again for every record. The maps
// stay referenced by the records until the transaction ends, so an identity
// cannot be reused by another map meanwhile.
func (w *writeTx) redactShared(m map[string]string) map[string]string {
	if !w.redact.Enabled() || len(m) == 0 {
		return m
	}
	key := reflect.ValueOf(m).UnsafePointer()
	if redacted, ok := w.shared[key]; ok {
		return redacted
	}
	if w.shared == nil {
		w.shared = make(map[unsafe.Pointer]map[string]string)
	}
	redacted := w.redact.Map(m)
	w.shared[key] = redacted
	return redacted
}

// inWriteTx runs fn in a write transaction. Writes are serialized by s.mu so
// concurrent writers never hit DuckDB transaction conflicts; reads take no lock
// and rely on DuckDB's MVCC instead.
func (s *DuckDBStore) inWriteTx(ctx context.Context, fn func(w *writeTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := fn(&writeTx{conn: conn, tx: tx, redact: s.redactor}); err != nil {
		return err
	}
	return tx.Commit()
}

// appendRows bulk-inserts n rows into the given columns of table. Appending is
// orders of magnitude faster than one INSERT per row.
func (w *writeTx) appendRows(ctx context.Context, table string, columns []string, n int, row func(i int) []driver.Value) error {
	if n == 0 {
		return nil
	}
	return w.conn.Raw(func(driverConn any) error {
		conn, ok := driverConn.(driver.Conn)
		if !ok {
			return fmt.Errorf("unexpected driver connection type %T", driverConn)
		}
		appender, err := duckdb.NewAppenderWithColumns(conn, "", "", table, columns)
		if err != nil {
			return fmt.Errorf("creating %s appender: %w", table, err)
		}
		for i := 0; i < n; i++ {
			if err := appender.AppendRow(row(i)...); err != nil {
				appender.Close()
				return fmt.Errorf("appending to %s: %w", table, err)
			}
		}
		if err := appender.CloseWithCancel(ctx); err != nil {
			return fmt.Errorf("flushing %s appender: %w", table, err)
		}
		return nil
	})
}

// rawJSON marks a serialized JSON document for an appender. Appenders marshal
// values written to JSON columns, which would store a plain string as a JSON
// string instead of the object or array it contains.
func rawJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}

// nullableString maps "" to NULL
func nullableString(s string) driver.Value {
	if s == "" {
		return nil
	}
	return s
}

// nullablePtr returns *p, or NULL for a nil pointer
func nullablePtr[T any](p *T) driver.Value {
	if p == nil {
		return nil
	}
	return *p
}
