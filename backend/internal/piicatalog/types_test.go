package piicatalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestColumnHash_NullVsEmptyComment(t *testing.T) {
	assert.NotEqual(t, ColumnHash(Column{Name: "snils"}), ColumnHash(Column{Name: "snils", Comment: new("")}))
}

func TestColumnHash_NoBoundaryAmbiguity(t *testing.T) {
	assert.NotEqual(t,
		ColumnHash(Column{Name: "a", Comment: new("b")}),
		ColumnHash(Column{Name: "a\x00\x01b"}))
}

func TestColumnHash_TableAndSchemaMatter(t *testing.T) {
	base := Column{Schema: "public", Table: "users", Name: "email"}
	other := []Column{
		{Schema: "public", Table: "clients", Name: "email"},
		{Schema: "archive", Table: "users", Name: "email"},
		{Schema: "public", Table: "users\x00email"},
	}

	for _, p := range other {
		assert.NotEqual(t, ColumnHash(base), ColumnHash(p), "%+v", p)
	}
}

func TestNewSnapshot_OrderIndependentAndDeduplicated(t *testing.T) {
	at := time.Now()
	a := NewSnapshot([]Column{{Name: "fio", Comment: new("ФИО")}, {Name: "id"}}, "h", 170000, at)
	b := NewSnapshot([]Column{{Name: "id"}, {Name: "fio", Comment: new("ФИО")}, {Name: "id"}}, "h", 170000, at)

	assert.Equal(t, a.SetHash, b.SetHash)
	assert.Len(t, b.Columns, 2)
}

func TestNewSnapshot_EmptySetHasStableHash(t *testing.T) {
	a := NewSnapshot(nil, "h", 0, time.Now())
	b := NewSnapshot([]Column{}, "h", 0, time.Now())

	assert.Equal(t, a.SetHash, b.SetHash)
	assert.NotEqual(t, a.SetHash, NewSnapshot([]Column{{Name: "id"}}, "h", 0, time.Now()).SetHash)
}
