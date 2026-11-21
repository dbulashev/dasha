package dto

import "github.com/dbulashev/dasha/internal/pkg/errs"

// ErrNotFound lives here rather than in repository so that domain packages,
// which may not import repository, can still recognise it. Packages that config
// imports must use errs.ErrNotFound: dto imports config.
var ErrNotFound = errs.ErrNotFound
