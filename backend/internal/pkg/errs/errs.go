// Package errs holds sentinels shared across layers. It imports nothing from the
// project, so any package can use it without an import cycle.
package errs

import "errors"

var ErrNotFound = errors.New("not found")
