package pg

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// pgDialect implements base.Dialect for PostgreSQL ($1, $2, ... placeholders).
var pgDialect base.Dialect = pgDialectImpl{}

type pgDialectImpl struct{}

func (pgDialectImpl) Placeholder(n int) string   { return fmt.Sprintf("$%d", n) }
func (pgDialectImpl) TransformValue(v any) any    { return v }
func (pgDialectImpl) SupportsReturning() bool     { return true }
