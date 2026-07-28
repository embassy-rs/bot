// Package pgtypes holds Go types for postgres columns that database/sql
// doesn't handle on its own.
package pgtypes

import (
	"database/sql"
	"database/sql/driver"

	"github.com/lib/pq"
)

// StringArray is a postgres text[].
//
// It is pq.StringArray with one difference: a nil slice writes as an empty
// array rather than NULL, so the column can be NOT NULL without every caller
// having to remember to initialize the field to a non-nil empty slice.
type StringArray []string

func (a *StringArray) Scan(src any) error {
	return (*pq.StringArray)(a).Scan(src)
}

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return pq.StringArray(a).Value()
}

var (
	_ driver.Valuer = StringArray(nil)
	_ sql.Scanner   = (*StringArray)(nil)
)
