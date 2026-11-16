package logs

import (
	"errors"
	"testing"
)

func TestPresetWord(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		id      string
		include []string
		want    string
		err     bool
	}{
		{name: "no preset", id: "", include: []string{"duration:"}, want: ""},
		{name: "slow", id: "slow", include: []string{"duration:"}, want: "duration"},
		{name: "plan", id: "plan", include: []string{"plan:"}, want: "plan"},
		{name: "case does not matter", id: "slow", include: []string{"DURATION:"}, want: "duration"},
		{name: "text extended", id: "slow", include: []string{"duration: 1"}, want: "duration"},
		{name: "one of several", id: "slow", include: []string{"select", "duration:"}, want: "duration"},
		{name: "text edited", id: "slow", include: []string{"durat"}, want: ""},
		{name: "text cleared", id: "slow", include: nil, want: ""},
		{name: "preset without word", id: "deadlock", include: []string{"deadlock"}, want: ""},
		{name: "unknown preset", id: "nope", include: []string{"duration:"}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := presetWord(tt.id, tt.include)
			if tt.err {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("err = %v", err)
			}

			if got != tt.want {
				t.Errorf("word = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every word must be a whole word of its substring, or a record holding the
// substring could miss the word.
func TestPresetWordsLieInTheirSubstrings(t *testing.T) {
	t.Parallel()

	for id, p := range searchPresets {
		if p.Word != "" && !containsFold(p.Substring, p.Word) {
			t.Errorf("preset %q: word %q not in %q", id, p.Word, p.Substring)
		}
	}
}
