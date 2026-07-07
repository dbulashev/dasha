package logs

import "testing"

func TestNormalize_MasksVariableParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		same bool // whether a and b should normalize to the same template
	}{
		{
			name: "durations differ only by number",
			a:    "login time: 656 microseconds",
			b:    "login time: 698 microseconds",
			same: true,
		},
		{
			name: "collapsed whitespace",
			a:    "duplicate   key   value",
			b:    "duplicate key value",
			same: true,
		},
		{
			name: "client addr and port masked",
			a:    "connection from 10.0.0.1:5432",
			b:    "connection from 192.168.1.2:6001",
			same: true,
		},
		{
			name: "quoted identifier masked",
			a:    `relation "orders" does not exist`,
			b:    `relation "customers" does not exist`,
			same: true,
		},
		{
			name: "structurally different stay distinct",
			a:    "login time: 656 microseconds",
			b:    "could not receive data from client",
			same: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSame := normalize(tt.a) == normalize(tt.b)
			if gotSame != tt.same {
				t.Errorf("normalize(%q)=%q vs normalize(%q)=%q: same=%v, want %v",
					tt.a, normalize(tt.a), tt.b, normalize(tt.b), gotSame, tt.same)
			}
		})
	}
}
