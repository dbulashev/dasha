package insights

import (
	"cmp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/dbulashev/dasha/internal/logs/pattern"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

const templateInputBytes = 1024

// TemplateCount is one message template inside a category.
type TemplateCount struct {
	Template    string
	Count       int
	First, Last time.Time
}

// CategorySummary is one category over a window. Share is the fraction of all
// classified records, 0..1.
type CategorySummary struct {
	Code        string
	Count       int
	Share       float64
	First, Last time.Time
	Templates   []TemplateCount
}

type categoryAcc struct {
	summary   CategorySummary
	templates map[string]*TemplateCount
}

// CategoryCounts accumulates classified records of one window.
type CategoryCounts struct {
	total  int
	byCode map[string]*categoryAcc
}

func NewCategoryCounts() *CategoryCounts {
	return &CategoryCounts{byCode: map[string]*categoryAcc{}}
}

// Add counts one record. The text of a plan record is not templated: the plan
// summary covers those.
func (c *CategoryCounts) Add(code, text string, ts time.Time) {
	c.total++

	acc, ok := c.byCode[code]
	if !ok {
		acc = &categoryAcc{
			summary:   CategorySummary{Code: code, First: ts, Last: ts},
			templates: map[string]*TemplateCount{},
		}
		c.byCode[code] = acc
	}

	acc.summary.Count++
	acc.summary.First, acc.summary.Last = widen(acc.summary.First, acc.summary.Last, ts)

	if code == CategoryPlan {
		return
	}

	tmpl := pattern.Display(sanitize.SQL(truncate(text, templateInputBytes)))

	t, ok := acc.templates[tmpl]
	if !ok {
		t = &TemplateCount{Template: tmpl, First: ts, Last: ts}
		acc.templates[tmpl] = t
	}

	t.Count++
	t.First, t.Last = widen(t.First, t.Last, ts)
}

// Summary lists the categories seen, most frequent first, each with at most
// topTemplates templates.
func (c *CategoryCounts) Summary(topTemplates int) []CategorySummary {
	out := make([]CategorySummary, 0, len(c.byCode))

	for _, acc := range c.byCode {
		s := acc.summary
		s.Share = float64(s.Count) / float64(c.total)

		s.Templates = make([]TemplateCount, 0, len(acc.templates))
		for _, t := range acc.templates {
			s.Templates = append(s.Templates, *t)
		}

		slices.SortFunc(s.Templates, func(a, b TemplateCount) int {
			return cmp.Or(cmp.Compare(b.Count, a.Count), b.Last.Compare(a.Last), cmp.Compare(a.Template, b.Template))
		})

		if len(s.Templates) > topTemplates {
			s.Templates = s.Templates[:topTemplates]
		}

		out = append(out, s)
	}

	slices.SortFunc(out, func(a, b CategorySummary) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Code, b.Code))
	})

	return out
}

func widen(first, last, ts time.Time) (time.Time, time.Time) {
	if ts.Before(first) {
		first = ts
	}

	if ts.After(last) {
		last = ts
	}

	return first, last
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n]
}
