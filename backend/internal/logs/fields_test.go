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
			name: "restartpoint complete with differing LSNs",
			a:    "restartpoint complete: wrote 18738 buffers (3.6%); 0 WAL file(s) added, 457 removed, 74 recycled; write=431.759 s, sync=0.006 s, total=432.048 s; lsn=2E/28E36B88, redo lsn=2D/159A2E80",
			b:    "restartpoint complete: wrote 350 buffers (0.1%); 0 WAL file(s) added, 16 removed, 0 recycled; write=109.165 s, sync=0.004 s, total=109.182 s; lsn=3A/9F00D428, redo lsn=3A/7C41B198",
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
