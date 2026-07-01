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

func TestExcludeReportsByUserEmptyExclude(t *testing.T) {
	t.Parallel()

	reports := []dto.QueryReport{{QueryID: 1, Usernames: []string{"reporting"}}} //nolint:exhaustruct

	if got := excludeReportsByUser(reports, nil); len(got) != 1 {
		t.Fatalf("empty exclude should keep all, got %d", len(got))
	}
}
