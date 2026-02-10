//go:build integration

package testinfra

import (
	"io"
	"strings"
)

// pgHbaReader returns a reader for pg_hba.conf that allows replication connections.
func pgHbaReader() io.Reader {
	return strings.NewReader(`# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             all                                     trust
host    all             all             0.0.0.0/0               trust
host    all             all             ::/0                    trust
local   replication     all                                     trust
host    replication     all             0.0.0.0/0               trust
host    replication     all             ::/0                    trust
`)
}
