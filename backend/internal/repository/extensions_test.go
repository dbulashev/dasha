package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQualify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		object string
		want   string
	}{
		{
			name:   "unknown schema leaves the object bare",
			schema: "",
			object: "pg_stat_statements",
			want:   "pg_stat_statements",
		},
		{
			name:   "custom schema is prefixed",
			schema: `"ext"`,
			object: "pg_stat_statements",
			want:   `"ext".pg_stat_statements`,
		},
		{
			name:   "reset function is qualified the same way",
			schema: `"monitoring"`,
			object: statsSourceDefs[0].Reset,
			want:   `"monitoring".pg_stat_statements_reset`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, qualify(tt.schema, tt.object))
		})
	}
}
