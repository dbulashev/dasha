package insights

import (
	"testing"

	"github.com/dbulashev/dasha/internal/logs/stream"
)

func pg(severity, state, text string) Record {
	return Record{Stream: stream.PostgreSQL, Severity: severity, SQLState: state, Text: text}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rec  Record
		want string
	}{
		{"deadlock by state", pg("ERROR", "40P01", "deadlock detected"), CategoryDeadlock},
		{"deadlock without a state field", pg("ERROR", "", "deadlock detected"), CategoryDeadlock},
		{"localized deadlock", pg("ОШИБКА", "40P01", "обнаружена взаимоблокировка"), CategoryDeadlock},
		{"state beats text", pg("ERROR", "XX000", "deadlock detected"), CategoryError},
		{"lock wait", pg("LOG", "00000", "process 4242 still waiting for ShareLock on transaction 777 after 1000.071 ms"), CategoryLockWait},
		{"lock acquired", pg("LOG", "", "process 4242 acquired ShareLock on transaction 777 after 2468.100 ms"), CategoryLockWait},
		{"lock timeout", pg("ERROR", "55P03", "canceling statement due to lock timeout"), CategoryLockWait},
		{"too many clients", pg("FATAL", "53300", "sorry, too many clients already"), CategoryConnectionLimit},
		{"reserved slots", pg("FATAL", "", "remaining connection slots are reserved for roles with the SUPERUSER attribute"), CategoryConnectionLimit},
		{"password failed", pg("FATAL", "28P01", `password authentication failed for user "app"`), CategoryAuthentication},
		{"no hba entry", pg("FATAL", "28000", `no pg_hba.conf entry for host "10.0.0.1", user "app", database "shop"`), CategoryAuthentication},
		{"statement timeout", pg("ERROR", "57014", "canceling statement due to statement timeout"), CategoryCanceled},
		{"plan", pg("LOG", "00000", "duration: 12.000 ms  plan:\nQuery Text: SELECT 1\nResult  (cost=0.00..0.01 rows=1 width=4)"), CategoryPlan},
		{"slow statement", pg("LOG", "00000", "duration: 1200.500 ms  statement: SELECT pg_sleep(1.2)"), CategorySlowQuery},
		{"slow execute", pg("LOG", "", "duration: 1200.500 ms  execute S_1: SELECT 1"), CategorySlowQuery},
		{"checkpoint", pg("LOG", "00000", "checkpoint complete: wrote 18738 buffers (3.6%)"), CategoryCheckpoint},
		{"restartpoint", pg("LOG", "", "restartpoint starting: time"), CategoryCheckpoint},
		{"autovacuum", pg("LOG", "", `automatic vacuum of table "shop.public.orders": index scans: 1`), CategoryAutovacuum},
		{"autoanalyze", pg("LOG", "", `automatic analyze of table "shop.public.orders"`), CategoryAutovacuum},
		{"aggressive vacuum", pg("LOG", "", `automatic aggressive vacuum to prevent wraparound of table "shop.public.orders"`), CategoryAutovacuum},
		{"temp file", pg("LOG", "00000", `temporary file: path "base/pgsql_tmp/pgsql_tmp4242.0", size 104857600`), CategoryTempFile},
		{"connection received", pg("LOG", "", "connection received: host=10.0.0.1 port=51234"), CategoryConnection},
		{"disconnection", pg("LOG", "", "disconnection: session time: 0:00:01.002 user=app database=shop host=10.0.0.1 port=51234"), CategoryConnection},
		{"error by state", pg("ERROR", "42P01", `relation "missing" does not exist`), CategoryError},
		{"fatal without a state field", pg("FATAL", "", "terminating connection due to administrator command"), CategoryError},
		{"warning is not an error", pg("WARNING", "01000", "some warning"), CategoryOther},
		{"plain log", pg("LOG", "00000", "database system is ready to accept connections"), CategoryOther},
		{"pooler has no postgres categories", Record{Stream: stream.Pooler, Severity: "info", Text: "checkpoint starting: time"}, CategoryOther},
		{"pooler error", Record{Stream: stream.Pooler, Severity: "error", Text: "server connection failed"}, CategoryError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Classify(tt.rec); got != tt.want {
				t.Errorf("Classify(%+v) = %s, want %s", tt.rec, got, tt.want)
			}
		})
	}
}

func TestClassifyCorpus(t *testing.T) {
	t.Parallel()

	for _, name := range corpora {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recs := loadCorpus(t, name)
			counts := map[string]int{}

			for _, r := range recs {
				code := Classify(r.classified())
				counts[code]++

				_, isPlan := Detect(r.Text, r.QueryID)
				if isPlan != (code == CategoryPlan) {
					t.Errorf("record %q classified %s, detector says plan=%v", r.Text[:min(len(r.Text), 40)], code, isPlan)
				}
			}

			if counts[CategoryPlan] != 14 || counts[CategorySlowQuery] != 2 {
				t.Errorf("plan = %d, slow_query = %d; want 14 and 2", counts[CategoryPlan], counts[CategorySlowQuery])
			}

			if counts[CategoryError] != 0 {
				t.Errorf("error = %d on a corpus without errors", counts[CategoryError])
			}

			if other := counts[CategoryOther]; other == 0 || other == len(recs) {
				t.Errorf("other = %d of %d; the corpus holds both kinds", other, len(recs))
			}
		})
	}
}

func TestCategoriesRegistry(t *testing.T) {
	t.Parallel()

	cats := Categories()
	seen := map[string]bool{}

	for _, c := range cats {
		if seen[c.Code] {
			t.Errorf("category %s registered twice", c.Code)
		}

		seen[c.Code] = true
	}

	if last := cats[len(cats)-1]; last.Code != CategoryOther {
		t.Errorf("last category = %s, want %s", last.Code, CategoryOther)
	}
}
