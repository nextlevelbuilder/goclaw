//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// sqliteTime wraps time.Time to handle SQLite's text-based timestamp storage.
// modernc.org/sqlite returns timestamps as strings; this type implements sql.Scanner
// to parse them automatically during rows.Scan().
type sqliteTime struct {
	Time time.Time
}

// Scan implements sql.Scanner for SQLite text timestamps.
func (st *sqliteTime) Scan(src any) error {
	if src == nil {
		st.Time = time.Time{}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		st.Time = v
		return nil
	case string:
		return st.parseString(v)
	case []byte:
		return st.parseString(string(v))
	default:
		return fmt.Errorf("sqliteTime: unsupported type %T", src)
	}
}

var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700 -07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func (st *sqliteTime) parseString(s string) error {
	// Strip Go monotonic clock suffix (e.g. " m=+12.492694459")
	if idx := strings.Index(s, " m="); idx > 0 {
		s = s[:idx]
	}
	for _, layout := range timeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			st.Time = t
			return nil
		}
	}
	return fmt.Errorf("sqliteTime: cannot parse %q", s)
}

// scanTimePair is a convenience for scanning created_at + updated_at columns.
// After scan, copy .Time back to the target time.Time fields.
func scanTimePair() (createdAt, updatedAt *sqliteTime) {
	return &sqliteTime{}, &sqliteTime{}
}

// nullSqliteTime wraps sql.NullTime equivalent for nullable timestamp columns.
type nullSqliteTime struct {
	Time  time.Time
	Valid bool
}

// Scan implements sql.Scanner for nullable SQLite text timestamps.
func (nt *nullSqliteTime) Scan(src any) error {
	if src == nil {
		nt.Time = time.Time{}
		nt.Valid = false
		return nil
	}
	st := &sqliteTime{}
	if err := st.Scan(src); err != nil {
		return err
	}
	nt.Time = st.Time
	nt.Valid = true
	return nil
}

// NullTime returns sql.NullTime from nullSqliteTime.
func (nt *nullSqliteTime) NullTime() sql.NullTime {
	return sql.NullTime{Time: nt.Time, Valid: nt.Valid}
}
