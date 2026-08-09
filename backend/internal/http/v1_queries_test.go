package http

import (
	"reflect"
	"testing"

	"github.com/dbulashev/dasha/internal/dto"
)

func TestExcludeReportsByUser(t *testing.T) {
	t.Parallel()

	reports := []dto.QueryReport{
		{QueryID: 1, Usernames: []string{"app"}},              //nolint:exhaustruct
		{QueryID: 2, Usernames: []string{"reporting"}},        //nolint:exhaustruct
		{QueryID: 3, Usernames: []string{"app", "reporting"}}, //nolint:exhaustruct
		{QueryID: 4, Usernames: nil},                          //nolint:exhaustruct
	}

	got := excludeReportsByUser(reports, []string{"reporting"})

	ids := make([]int64, 0, len(got))
	for _, r := range got {
		ids = append(ids, r.QueryID)
	}

	// 2 dropped (solely excluded); 3 kept (shared), 4 kept (unknown attribution).
	if want := []int64{1, 3, 4}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("got ids %v, want %v", ids, want)
	}
}

func TestResolveScope(t *testing.T) {
	t.Parallel()

	instance := dto.ScopeInstance
	database := dto.ScopeDatabase

	cases := []struct {
		name     string
		scope    *string
		database string
		want     string
	}{
		{"no database can only be instance-wide", nil, "", dto.ScopeInstance},
		{"database defaults to its own scope", nil, "app", dto.ScopeDatabase},
		{"explicit instance wins over a named database", &instance, "app", dto.ScopeInstance},
		{"explicit database without one falls back", &database, "", dto.ScopeInstance},
	}

	for _, c := range cases {
		if got := resolveScope(c.scope, c.database); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestScopeReportDatabase(t *testing.T) {
	t.Parallel()

	dbPct, instPct := 60.0, 12.0
	reports := []dto.QueryReport{
		{QueryID: 1, Datname: "app", TotalTimePct: &dbPct, TotalTimePctInstance: &instPct}, //nolint:exhaustruct
		{QueryID: 2, Datname: "other"}, //nolint:exhaustruct
		{QueryID: 3, Datname: ""},      //nolint:exhaustruct
	}

	got := scopeReport(reports, dto.ScopeDatabase, "app")
	if len(got) != 1 || got[0].QueryID != 1 {
		t.Fatalf("expected only the app row, got %+v", got)
	}

	// Database scope keeps the shares the SQL computed within the database.
	if *got[0].TotalTimePct != dbPct {
		t.Fatalf("got share %v, want %v", *got[0].TotalTimePct, dbPct)
	}
}

func TestScopeReportInstanceSwapsShares(t *testing.T) {
	t.Parallel()

	dbPct, instPct := 60.0, 12.0
	reports := []dto.QueryReport{
		{QueryID: 1, Datname: "app", TotalTimePct: &dbPct, TotalTimePctInstance: &instPct}, //nolint:exhaustruct
		{QueryID: 2, Datname: "other"}, //nolint:exhaustruct
	}

	got := scopeReport(reports, dto.ScopeInstance, "app")
	if len(got) != 2 {
		t.Fatalf("instance scope must keep every database, got %d rows", len(got))
	}

	if *got[0].TotalTimePct != instPct {
		t.Fatalf("got share %v, want the instance-wide %v", *got[0].TotalTimePct, instPct)
	}
}

func TestCompareScope(t *testing.T) {
	t.Parallel()

	instance := dto.ScopeInstance

	cases := []struct {
		name             string
		attribA, attribB bool
		scope            *string
		database         string
		want             string
		wantOK           bool
	}{
		{"both attributed follow the requested scope", true, true, nil, "app", dto.ScopeDatabase, true},
		{"both attributed honour an explicit instance", true, true, &instance, "app", dto.ScopeInstance, true},
		{"two legacy sides compare instance-wide", false, false, nil, "app", dto.ScopeInstance, true},
		{"legacy A against live or a newer B is refused", false, true, nil, "app", "", false},
		{"newer A against a legacy B is refused", true, false, nil, "app", "", false},
	}

	for _, c := range cases {
		got, ok := compareScope(c.attribA, c.attribB, c.scope, c.database)
		if ok != c.wantOK || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestReportKeyOfSeparatesDatabases(t *testing.T) {
	t.Parallel()

	a := reportKeyOf(dto.QueryReport{QueryID: 42, Datname: "app"})   //nolint:exhaustruct
	b := reportKeyOf(dto.QueryReport{QueryID: 42, Datname: "other"}) //nolint:exhaustruct

	if a == b {
		t.Fatal("the same queryid in two databases must not share a key")
	}
}

func TestExcludeReportsByUserEmptyExclude(t *testing.T) {
	t.Parallel()

	reports := []dto.QueryReport{{QueryID: 1, Usernames: []string{"reporting"}}} //nolint:exhaustruct

	if got := excludeReportsByUser(reports, nil); len(got) != 1 {
		t.Fatalf("empty exclude should keep all, got %d", len(got))
	}
}
