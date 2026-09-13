package insights

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/logs/pattern"
)

var t0 = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func TestCategoriesSummary(t *testing.T) {
	t.Parallel()

	c := NewCategoryCounts()
	c.Add(CategoryTempFile, `temporary file: path "base/pgsql_tmp/pgsql_tmp1.0", size 1024`, at(5))
	c.Add(CategoryTempFile, `temporary file: path "base/pgsql_tmp/pgsql_tmp2.0", size 2048`, at(1))
	c.Add(CategoryTempFile, `temporary file: path "base/pgsql_tmp/pgsql_tmp3.0", size 4096`, at(9))
	c.Add(CategoryCheckpoint, "checkpoint starting: time", at(3))
	c.Add(CategoryPlan, "duration: 1.000 ms  plan:\nQuery Text: SELECT 1", at(4))

	sum := c.Summary(5)
	if len(sum) != 3 {
		t.Fatalf("got %d categories, want 3", len(sum))
	}

	top := sum[0]
	if top.Code != CategoryTempFile || top.Count != 3 {
		t.Fatalf("top = %s×%d, want temp_file×3", top.Code, top.Count)
	}

	if !top.First.Equal(at(1)) || !top.Last.Equal(at(9)) {
		t.Errorf("first/last = %v/%v, want %v/%v", top.First, top.Last, at(1), at(9))
	}

	if len(top.Templates) != 1 || top.Templates[0].Count != 3 {
		t.Fatalf("templates = %+v, want one template counted 3 times", top.Templates)
	}

	if !strings.Contains(top.Templates[0].Template, pattern.Placeholder) {
		t.Errorf("template %q is not masked", top.Templates[0].Template)
	}

	var share float64
	for _, s := range sum {
		share += s.Share
	}

	if math.Abs(share-1) > 1e-9 {
		t.Errorf("shares add up to %v, want 1", share)
	}

	for _, s := range sum {
		if s.Code == CategoryPlan && len(s.Templates) != 0 {
			t.Errorf("plan category carries templates %+v", s.Templates)
		}
	}
}

func TestCategoriesKeepTheTopTemplates(t *testing.T) {
	t.Parallel()

	c := NewCategoryCounts()

	for i, text := range []string{"a one", "b two", "b two", "c three", "c three", "c three"} {
		c.Add(CategoryOther, text, at(i))
	}

	sum := c.Summary(2)

	got := sum[0].Templates
	if len(got) != 2 || got[0].Template != "c three" || got[1].Template != "b two" {
		t.Errorf("templates = %+v, want c three then b two", got)
	}

	if sum[0].Count != 6 {
		t.Errorf("count = %d, want every record counted", sum[0].Count)
	}
}

func TestCategoriesMaskCredentials(t *testing.T) {
	t.Parallel()

	c := NewCategoryCounts()
	c.Add(CategoryError, "could not connect: host=db password=hunter2 user=app", at(0))

	tmpl := c.Summary(1)[0].Templates[0].Template
	if strings.Contains(tmpl, "hunter2") {
		t.Errorf("template %q leaks the password", tmpl)
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	t.Parallel()

	s := "ошибка"

	for n := range len(s) + 1 {
		if got := truncate(s, n); !strings.HasPrefix(s, got) || len(got) > n || len(got)%2 != 0 {
			t.Errorf("truncate(%q, %d) = %q", s, n, got)
		}
	}
}
