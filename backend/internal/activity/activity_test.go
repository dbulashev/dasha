package activity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		s    Session
		want string
	}{
		{"idle in transaction waiting on ClientRead", Session{State: "idle in transaction", WaitType: "Client", WaitEvent: "ClientRead"}, ClassClientIdleTx},
		{"aborted transaction", Session{State: "idle in transaction (aborted)", WaitType: "Client"}, ClassClientIdleTx},
		{"active without wait", Session{State: "active"}, ClassCPU},
		{"active on lock", Session{State: "active", WaitType: "Lock", WaitEvent: "relation"}, ClassLock},
		{"active on LWLock", Session{State: "active", WaitType: "LWLock"}, ClassLWLock},
		{"active on IO", Session{State: "active", WaitType: "IO", WaitEvent: "DataFileRead"}, ClassIO},
		{"active on ClientRead", Session{State: "active", WaitType: "Client", WaitEvent: "ClientRead"}, ClassClient},
		{"background idle loop", Session{BackendType: "checkpointer", WaitType: "Activity"}, ClassActivity},
		{"unknown wait type", Session{State: "active", WaitType: "InjectionPoint"}, ClassOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Class(tc.s))
		})
	}
}

func TestClassesCoverEveryClass(t *testing.T) {
	t.Parallel()

	for _, c := range waitTypeClasses {
		assert.Contains(t, Classes, c)
	}

	for _, c := range []string{ClassCPU, ClassClientIdleTx, ClassOther} {
		assert.Contains(t, Classes, c)
	}
}

func TestCounted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		s          Session
		background bool
		want       bool
	}{
		{"client active", Session{BackendType: clientBackend, State: "active"}, false, true},
		{"client idle in transaction", Session{BackendType: clientBackend, State: "idle in transaction"}, false, true},
		{"client aborted", Session{BackendType: clientBackend, State: "idle in transaction (aborted)"}, false, true},
		{"client fastpath", Session{BackendType: clientBackend, State: "fastpath function call"}, false, true},
		{"client disabled", Session{BackendType: clientBackend, State: "disabled"}, true, false},
		{"background without flag", Session{BackendType: "autovacuum worker", WaitType: "IO"}, false, false},
		{"background working", Session{BackendType: "autovacuum worker", WaitType: "IO"}, true, true},
		{"background on CPU", Session{BackendType: "walwriter"}, true, true},
		{"background idle loop", Session{BackendType: "checkpointer", WaitType: "Activity"}, true, false},
		{"parallel worker active", Session{BackendType: parallelWorker, State: "active", WaitType: "IO"}, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Counted(tc.s, tc.background))
		})
	}
}

type fakeFingerprinter struct {
	fp  string
	err error
}

func (f fakeFingerprinter) Fingerprint(string) (string, error) { return f.fp, f.err }

func TestQueryKey(t *testing.T) {
	t.Parallel()

	ok := fakeFingerprinter{fp: "abc123"}
	bad := fakeFingerprinter{err: errors.New("parse error")}

	key, kind := QueryKey(Session{QueryID: -42, Text: "select 1"}, ok)
	assert.Equal(t, "q:-42", key)
	assert.Equal(t, KeyKindQueryID, kind)

	key, kind = QueryKey(Session{Text: "select 1"}, ok)
	assert.Equal(t, "f:abc123", key)
	assert.Equal(t, KeyKindFingerprint, kind)

	key, kind = QueryKey(Session{Text: "select * from t where a = 1 and b = 'x"}, bad)
	assert.True(t, strings.HasPrefix(key, "t:"), key)
	assert.Equal(t, KeyKindText, kind)

	for _, text := range []string{"", "   ", "<insufficient privilege>"} {
		key, kind = QueryKey(Session{Text: text}, ok)
		assert.Empty(t, key, text)
		assert.Equal(t, KeyKindNone, kind, text)
	}
}

func TestQueryKeyTextMasksLiterals(t *testing.T) {
	t.Parallel()

	bad := fakeFingerprinter{err: errors.New("truncated")}

	a, _ := QueryKey(Session{Text: "select * from t where id = 1 and name = 'bob' and x in (sel"}, bad)
	b, _ := QueryKey(Session{Text: "select * from t where id = 25 and name = 'alice' and x in (sel"}, bad)
	c, _ := QueryKey(Session{Text: "select * from u where id = 1"}, bad)

	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)

	prefix := "select * from t where " + strings.Repeat("a and ", 60)
	d, _ := QueryKey(Session{Text: prefix + "tail one"}, bad)
	e, _ := QueryKey(Session{Text: prefix + "tail two"}, bad)
	assert.NotEqual(t, d, e, "a shared long prefix does not merge statements")

	f, _ := QueryKey(Session{Text: "select * from t where id in (1, 2, 3) and x in (sel"}, bad)
	g, _ := QueryKey(Session{Text: "select * from t where id in (7) and x in (sel"}, bad)
	assert.Equal(t, f, g, "lists of literals collapse")

	h, _ := QueryKey(Session{Text: "insert into t values (1, 'a'), (2, 'b'), (3, 'c'); sel"}, bad)
	i, _ := QueryKey(Session{Text: "insert into t values (4, 'd'); sel"}, bad)
	assert.Equal(t, h, i, "VALUES rows collapse")
}

func TestQueryIDAvailable(t *testing.T) {
	t.Parallel()

	assert.True(t, QueryIDAvailable("on", false))
	assert.True(t, QueryIDAvailable("regress", false))
	assert.True(t, QueryIDAvailable("auto", true))
	assert.False(t, QueryIDAvailable("auto", false))
	assert.False(t, QueryIDAvailable("off", true))
}

func TestTruncateText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc", TruncateText("abc", 5))
	assert.Equal(t, "ab", TruncateText("abc", 2))
	assert.Equal(t, "жё", TruncateText("жёлтый", 2))
	assert.Equal(t, "жёлтый", TruncateText("жёлтый", 6))
}

func TestConfig(t *testing.T) {
	t.Parallel()

	def := Config{}.WithDefaults()
	require.NoError(t, def.Validate())
	assert.Equal(t, DefaultWindowMinutes, def.WindowMinutes)
	assert.Equal(t, DefaultSampleTimeout, def.SampleTimeout)
	assert.Equal(t, DefaultMaxSessions, def.MaxSessions)

	bad := []Config{
		{WindowMinutes: 4, SampleTimeout: time.Second, MaxSessions: 2000},
		{WindowMinutes: 241, SampleTimeout: time.Second, MaxSessions: 2000},
		{WindowMinutes: 60, SampleTimeout: 100 * time.Millisecond, MaxSessions: 2000},
		{WindowMinutes: 60, SampleTimeout: 11 * time.Second, MaxSessions: 2000},
		{WindowMinutes: 60, SampleTimeout: time.Second, MaxSessions: 99},
		{WindowMinutes: 60, SampleTimeout: time.Second, MaxSessions: 20001},
	}

	for _, c := range bad {
		assert.Error(t, c.Validate(), "%+v", c)
	}
}
