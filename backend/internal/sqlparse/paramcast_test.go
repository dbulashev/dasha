package sqlparse

import "testing"

// Every sql here is what pg_stat_statements stores for a statement written with
// a `type 'literal'` constant, and every want is valid SQL — TestParseAcceptsTypePrefixedParams
// runs the same corpus through the grammar.
var typePrefixedCorpus = []struct {
	name string
	sql  string
	want string
}{
	{
		name: "timestamptz",
		sql:  `SELECT count(*) FROM orders_2024 WHERE created_at > timestamptz $1`,
		want: `SELECT count(*) FROM orders_2024 WHERE created_at > $1::timestamptz`,
	},
	{
		name: "spelled out with time zone",
		sql:  `SELECT * FROM orders WHERE created_at > timestamp with time zone $1`,
		want: `SELECT * FROM orders WHERE created_at > $1::timestamp with time zone`,
	},
	{
		name: "precision and without time zone",
		sql:  `SELECT * FROM orders WHERE created_at > timestamp(3) without time zone $1`,
		want: `SELECT * FROM orders WHERE created_at > $1::timestamp(3) without time zone`,
	},
	{
		name: "numeric typmod",
		sql:  `SELECT * FROM orders WHERE amount > numeric(10,2) $1`,
		want: `SELECT * FROM orders WHERE amount > $1::numeric(10,2)`,
	},
	{
		name: "character varying",
		sql:  `SELECT * FROM users WHERE name = character varying(10) $1`,
		want: `SELECT * FROM users WHERE name = $1::character varying(10)`,
	},
	{
		name: "date",
		sql:  `SELECT * FROM events WHERE d = date $1`,
		want: `SELECT * FROM events WHERE d = $1::date`,
	},
	{
		name: "boolean",
		sql:  `SELECT * FROM events WHERE flag = boolean $1`,
		want: `SELECT * FROM events WHERE flag = $1::boolean`,
	},
	{
		name: "schema qualified type",
		sql:  `SELECT * FROM events WHERE d = pg_catalog.date $1`,
		want: `SELECT * FROM events WHERE d = $1::pg_catalog.date`,
	},
	{
		name: "case is irrelevant",
		sql:  `SELECT * FROM orders WHERE created_at > TIMESTAMPTZ $1`,
		want: `SELECT * FROM orders WHERE created_at > $1::TIMESTAMPTZ`,
	},
	{
		name: "both sides of between",
		sql:  `SELECT * FROM orders WHERE created_at BETWEEN timestamptz $1 AND timestamptz $2`,
		want: `SELECT * FROM orders WHERE created_at BETWEEN $1::timestamptz AND $2::timestamptz`,
	},
	{
		name: "interval",
		sql:  `SELECT * FROM events WHERE ts > now() - interval $1`,
		want: `SELECT * FROM events WHERE ts > now() - CAST($1 AS interval)`,
	},
	{
		name: "interval keeps its qualifier",
		sql:  `SELECT * FROM events WHERE ts > now() - interval $1 day to second`,
		want: `SELECT * FROM events WHERE ts > now() - CAST($1 AS interval day to second)`,
	},
	{
		name: "interval precision",
		sql:  `SELECT * FROM events WHERE ts > now() - interval (3) $1`,
		want: `SELECT * FROM events WHERE ts > now() - CAST($1 AS interval (3))`,
	},
	{
		name: "an alias is not an interval qualifier",
		sql:  `SELECT interval $1 day_offset FROM events`,
		want: `SELECT CAST($1 AS interval) day_offset FROM events`,
	},
}

func TestRestoreParamCastsRewritesTypePrefixedParams(t *testing.T) {
	for _, tc := range typePrefixedCorpus {
		t.Run(tc.name, func(t *testing.T) {
			if got := RestoreParamCasts(tc.sql); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A keyword in front of a parameter has the same shape as a type name and is the
// one thing this rewrite must never touch.
func TestRestoreParamCastsLeavesValidSqlAlone(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"limit and offset", `SELECT * FROM orders ORDER BY id LIMIT $1 OFFSET $2`},
		{"like and escape", `SELECT * FROM users WHERE email LIKE $1 ESCAPE $2`},
		{"between", `SELECT * FROM orders WHERE created_at BETWEEN $1 AND $2`},
		{"at time zone", `SELECT created_at AT TIME ZONE $1 FROM orders`},
		{"fetch first", `SELECT * FROM orders ORDER BY id FETCH FIRST $1 ROWS ONLY`},
		{"already a cast", `SELECT * FROM orders WHERE created_at > $1::timestamptz`},
		{"no type prefix", `SELECT * FROM orders WHERE created_at > $1`},
		{"type name is only a suffix", `SELECT * FROM t WHERE x = mytimestamptz $1`},
		{"no parameters at all", `SELECT * FROM orders WHERE created_at > timestamptz '2024-01-01'`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RestoreParamCasts(tc.sql); got != tc.sql {
				t.Errorf("rewritten to %s", got)
			}
		})
	}
}

func TestRestoreParamCastsIsIdempotent(t *testing.T) {
	for _, tc := range typePrefixedCorpus {
		t.Run(tc.name, func(t *testing.T) {
			if got := RestoreParamCasts(tc.want); got != tc.want {
				t.Errorf("second pass changed it to %s", got)
			}
		})
	}
}

// The grammar is the only judge of whether the rewrite produced SQL, and Parse
// is what has to accept the text pg_stat_statements actually stores.
func TestParseAcceptsTypePrefixedParams(t *testing.T) {
	p := New(Config{})

	for _, tc := range typePrefixedCorpus {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.Parse(tc.sql); err != nil {
				t.Fatalf("parse: %v", err)
			}

			if _, err := p.Fingerprint(tc.sql); err != nil {
				t.Fatalf("fingerprint: %v", err)
			}
		})
	}
}

// The cast lands on the parameter, so the predicate stays a plain range on the
// column and still yields an index candidate.
func TestParseKeepsColumnUsageAfterRestore(t *testing.T) {
	p := New(Config{})

	cases := []struct {
		name   string
		sql    string
		usages []string
	}{
		{
			name:   "type prefixed bound",
			sql:    `SELECT count(*) FROM orders_2024 WHERE created_at > timestamptz $1`,
			usages: []string{"orders_2024.created_at range"},
		},
		{
			name:   "interval arithmetic on the bound",
			sql:    `SELECT * FROM events WHERE tenant_id = $1 AND ts > now() - interval $2`,
			usages: []string{"events.tenant_id equality", "events.ts range"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := p.Parse(tc.sql)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			assertSet(t, "usages", usageStrings(st.Usages), tc.usages)

			if len(st.Unsupported) != 0 {
				t.Errorf("unsupported = %v, want none", st.Unsupported)
			}
		})
	}
}
