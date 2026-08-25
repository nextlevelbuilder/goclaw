//go:build sqlite || sqliteonly

package sqlitestore

import "fmt"

// sqliteJSONValue accepts JSON stored by SQLite as either TEXT or BLOB.
type sqliteJSONValue []byte

// Scan implements sql.Scanner.
func (j *sqliteJSONValue) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*j = nil
	case string:
		*j = append((*j)[:0], value...)
	case []byte:
		*j = append((*j)[:0], value...)
	default:
		return fmt.Errorf("sqliteJSONValue: unsupported type %T", src)
	}
	return nil
}
