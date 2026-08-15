package sqlparse

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The corpus is the contract of this package: for each query text, exactly which
// tables, column usages, write targets and skip reasons come out of it. Every
// shape here appears in a real pg_stat_statements top — the normalized ones
// (= ANY($1) instead of IN, $n instead of literals) doubly so.
type corpusCase struct {
	name    string
	sql     string
	kind    Kind
	tables  []string
	usages  []string
	written []string
	skipped []string
}

func TestDescribeCorpus(t *testing.T) {
	cases := []corpusCase{
		{
			name:   "equality on a single table",
			sql:    `SELECT id, email FROM users WHERE id = $1`,
			kind:   KindSelect,
			tables: []string{"users"},
			usages: []string{"users.id equality"},
		},
		{
			name:   "in list as pg_stat_statements normalizes it",
			sql:    `SELECT * FROM public.orders o WHERE o.customer_id = ANY ($1) AND o.status = $2`,
			kind:   KindSelect,
			tables: []string{"public.orders"},
			usages: []string{"public.orders.customer_id equality", "public.orders.status equality"},
		},
		{
			name:   "literal in list",
			sql:    `SELECT * FROM orders WHERE status IN ($1, $2, $3)`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.status equality"},
		},
		{
			name:   "between is a range, order by keeps its direction",
			sql:    `SELECT * FROM orders WHERE created_at BETWEEN $1 AND $2 ORDER BY created_at DESC`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.created_at range", "orders.created_at order#0 desc"},
		},
		{
			name:   "comparison operators are ranges",
			sql:    `SELECT * FROM orders WHERE amount >= $1 AND created_at < $2`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.amount range", "orders.created_at range"},
		},
		{
			name:   "is null counts as equality, is not null does not",
			sql:    `SELECT * FROM orders WHERE deleted_at IS NULL AND tenant_id = $1 AND shipped_at IS NOT NULL`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.deleted_at equality", "orders.tenant_id equality"},
		},
		{
			name:   "join on resolves both aliases",
			sql:    `SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id WHERE c.status = $1`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"customers.id join", "orders.customer_id join", "customers.status equality"},
		},
		{
			name:   "join using attributes the column to both sides",
			sql:    `SELECT * FROM orders JOIN customers USING (customer_id)`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"orders.customer_id join", "customers.customer_id join"},
		},
		{
			name:   "group by keeps its position",
			sql:    `SELECT customer_id, status, count(*) FROM orders WHERE created_at > $1 GROUP BY customer_id, status`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{
				"orders.created_at range",
				"orders.customer_id group#0",
				"orders.status group#1",
			},
		},
		{
			name:   "subquery in predicate and its own where",
			sql:    `SELECT * FROM orders WHERE customer_id IN (SELECT id FROM customers WHERE country = $1)`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"orders.customer_id equality", "customers.country equality"},
		},
		{
			name: "cte name is not a table",
			sql: `WITH recent AS (SELECT id FROM orders WHERE created_at > $1)
			      SELECT * FROM recent r JOIN customers c ON c.id = r.id WHERE c.status = $2`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"orders.created_at range", "customers.id join", "customers.status equality"},
		},
		{
			name: "lateral subquery sees the outer table",
			sql: `SELECT * FROM customers c, LATERAL (
			        SELECT id FROM orders o WHERE o.customer_id = c.id ORDER BY o.created_at DESC LIMIT $1
			      ) x`,
			kind:   KindSelect,
			tables: []string{"customers", "orders"},
			usages: []string{
				"orders.customer_id join",
				"customers.id join",
				"orders.created_at order#0 desc",
			},
		},
		{
			name: "each arm of a union is analyzed on its own",
			sql: `SELECT id FROM orders WHERE status = $1
			      UNION ALL
			      SELECT id FROM archived_orders WHERE status = $2`,
			kind:   KindSelect,
			tables: []string{"orders", "archived_orders"},
			usages: []string{"orders.status equality", "archived_orders.status equality"},
		},
		{
			name:   "window clause yields nothing, the where still does",
			sql:    `SELECT row_number() OVER (PARTITION BY customer_id ORDER BY created_at DESC) FROM orders WHERE status = $1`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.status equality"},
		},
		{
			name:   "self join keeps one table and both roles",
			sql:    `SELECT * FROM employees e JOIN employees m ON m.id = e.manager_id WHERE e.dept_id = $1`,
			kind:   KindSelect,
			tables: []string{"employees"},
			usages: []string{"employees.id join", "employees.manager_id join", "employees.dept_id equality"},
		},
		{
			name:   "bare column with several tables in scope stays unattributed",
			sql:    `SELECT * FROM orders o JOIN customers c ON c.id = o.customer_id WHERE status = $1`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"customers.id join", "orders.customer_id join", "?.status equality"},
		},
		{
			name:   "subquery in from occupies a slot",
			sql:    `SELECT * FROM (SELECT id, status FROM orders WHERE created_at > $1) t WHERE status = $2`,
			kind:   KindSelect,
			tables: []string{"orders"},
			usages: []string{"orders.created_at range", "?.status equality"},
		},
		{
			name:    "update writes and filters",
			sql:     `UPDATE orders SET status = $1 WHERE id = $2 AND tenant_id = $3`,
			kind:    KindUpdate,
			tables:  []string{"orders"},
			written: []string{"orders"},
			usages:  []string{"orders.id equality", "orders.tenant_id equality"},
		},
		{
			name:    "delete writes and filters",
			sql:     `DELETE FROM sessions WHERE expires_at < $1`,
			kind:    KindDelete,
			tables:  []string{"sessions"},
			written: []string{"sessions"},
			usages:  []string{"sessions.expires_at range"},
		},
		{
			name:    "insert is write cost only",
			sql:     `INSERT INTO audit_log (id, payload) VALUES ($1, $2)`,
			kind:    KindInsert,
			tables:  []string{"audit_log"},
			written: []string{"audit_log"},
		},
		{
			name: "merge reports its join condition",
			sql: `MERGE INTO stock s USING shipments sh ON s.sku = sh.sku
			      WHEN MATCHED THEN UPDATE SET qty = s.qty + sh.qty`,
			kind:    KindMerge,
			tables:  []string{"stock", "shipments"},
			written: []string{"stock"},
			usages:  []string{"stock.sku join", "shipments.sku join"},
		},
		{
			name:    "function on the column is not a btree predicate",
			sql:     `SELECT * FROM users WHERE lower(email) = $1`,
			kind:    KindSelect,
			tables:  []string{"users"},
			skipped: []string{ReasonExpressionPredicate},
		},
		{
			name:    "cast on the column is not a btree predicate",
			sql:     `SELECT * FROM users WHERE id::text = $1`,
			kind:    KindSelect,
			tables:  []string{"users"},
			skipped: []string{ReasonExpressionPredicate},
		},
		{
			name:    "or branches are skipped with a reason",
			sql:     `SELECT * FROM users WHERE email = $1 OR phone = $2`,
			kind:    KindSelect,
			tables:  []string{"users"},
			skipped: []string{ReasonOrPredicate},
		},
		{
			name:    "an and-connected predicate survives an or branch",
			sql:     `SELECT * FROM orders WHERE tenant_id = $1 AND (status = $2 OR status = $3)`,
			kind:    KindSelect,
			tables:  []string{"orders"},
			usages:  []string{"orders.tenant_id equality"},
			skipped: []string{ReasonOrPredicate},
		},
		{
			name:   "subquery inside an or branch is still analyzed",
			sql:    `SELECT * FROM orders WHERE tenant_id = $1 OR customer_id IN (SELECT id FROM customers WHERE country = $2)`,
			kind:   KindSelect,
			tables: []string{"orders", "customers"},
			usages: []string{"customers.country equality"},
			// The IN itself lives under the OR, so only the subquery's own
			// predicate survives.
			skipped: []string{ReasonOrPredicate},
		},
		{
			name: "set is utility",
			sql:  `SET search_path TO $1`,
			kind: KindUtility,
		},
		{
			name: "ddl is utility",
			sql:  `CREATE INDEX CONCURRENTLY idx ON orders (customer_id)`,
			kind: KindUtility,
		},
	}

	p := New(Config{})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := p.Parse(tc.sql)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if st.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", st.Kind, tc.kind)
			}

			if st.Fingerprint == "" {
				t.Error("fingerprint is empty")
			}

			assertSet(t, "tables", refStrings(st.Tables), tc.tables)
			assertSet(t, "written", refStrings(st.Written), tc.written)
			assertSet(t, "usages", usageStrings(st.Usages), tc.usages)
			assertSet(t, "skipped", st.Unsupported, tc.skipped)
		})
	}
}

// TestDescribeUsageOrderIsStable pins the walk order, because the caller builds
// index candidates by reading equality columns before range ones and falls back
// to statement order when the catalog has no statistics to sort by.
func TestDescribeUsageOrderIsStable(t *testing.T) {
	const sql = `SELECT * FROM orders
	             WHERE tenant_id = $1 AND created_at > $2
	             ORDER BY created_at DESC`

	p := New(Config{})

	st, err := p.Parse(sql)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []string{
		"orders.tenant_id equality",
		"orders.created_at range",
		"orders.created_at order#0 desc",
	}

	got := usageStrings(st.Usages)
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("usages =\n  %v\nwant\n  %v", got, want)
	}
}

func refStrings(refs []Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, fmtRef(r))
	}

	return out
}

func usageStrings(usages []Usage) []string {
	out := make([]string, 0, len(usages))

	for _, u := range usages {
		s := fmt.Sprintf("%s.%s %s", fmtRef(u.Ref), u.Column, u.Role)

		if u.Role == RoleOrder || u.Role == RoleGroup {
			s += fmt.Sprintf("#%d", u.Seq)
		}

		if u.Desc {
			s += " desc"
		}

		out = append(out, s)
	}

	return out
}

func fmtRef(r Ref) string {
	switch {
	case r.Name == "":
		return "?"
	case r.Schema == "":
		return r.Name
	default:
		return r.Schema + "." + r.Name
	}
}

func assertSet(t *testing.T, what string, got, want []string) {
	t.Helper()

	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)

	if len(g) != len(w) {
		t.Errorf("%s = %v, want %v", what, got, want)

		return
	}

	for i := range g {
		if g[i] != w[i] {
			t.Errorf("%s = %v, want %v", what, got, want)

			return
		}
	}
}
