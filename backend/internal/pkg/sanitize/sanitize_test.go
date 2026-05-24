package sanitize

import "testing"

func TestSQL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "CREATE SUBSCRIPTION with connection string",
			in:   "CREATE SUBSCRIPTION orders_sub CONNECTION 'host=pg18-master port=5432 dbname=demo user=demo password=demo' PUBLICATION orders_pub",
			want: "CREATE SUBSCRIPTION orders_sub CONNECTION 'host=pg18-master port=5432 dbname=demo user=demo password=***' PUBLICATION orders_pub",
		},
		{
			name: "CREATE ROLE with PASSWORD",
			in:   "CREATE ROLE foo WITH LOGIN PASSWORD 'super_secret'",
			want: "CREATE ROLE foo WITH LOGIN PASSWORD '***'",
		},
		{
			name: "ALTER USER with PASSWORD double quotes",
			in:   `ALTER USER admin PASSWORD "s3cret"`,
			want: `ALTER USER admin PASSWORD '***'`,
		},
		{
			name: "libpq connection string",
			in:   "host=db.example.com port=5432 user=app password=p@ss123 dbname=prod",
			want: "host=db.example.com port=5432 user=app password=*** dbname=prod",
		},
		{
			name: "no credentials",
			in:   "SELECT * FROM orders WHERE id = 1",
			want: "SELECT * FROM orders WHERE id = 1",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SQL(tt.in)
			if got != tt.want {
				t.Errorf("SQL() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}
