package dto

import "errors"

// ErrNotFound lives here rather than in repository so that domain packages,
// which may not import repository, can still recognise it.
var ErrNotFound = errors.New("not found")
