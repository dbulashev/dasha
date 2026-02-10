// Package filter implements regexp matching for cluster and database names.
package filter

import (
	"fmt"
	"regexp"
)

// Filter implements regexp filters for clusters and their databases.
type Filter struct {
	nameRegexp        *regexp.Regexp
	dbRegexp          *regexp.Regexp
	excludeNameRegexp *regexp.Regexp
	excludeDbRegexp   *regexp.Regexp
}

// New returns a Filter with compiled regexps.
func New(name string, db, excludeName, excludeDb *string) (*Filter, error) {
	f := &Filter{} //nolint:exhaustruct

	var err error

	f.nameRegexp, err = regexp.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("compile name regexp %q: %w", name, err)
	}

	if db != nil {
		f.dbRegexp, err = regexp.Compile(*db)
		if err != nil {
			return nil, fmt.Errorf("compile db regexp %q: %w", *db, err)
		}
	}

	if excludeName != nil {
		f.excludeNameRegexp, err = regexp.Compile(*excludeName)
		if err != nil {
			return nil, fmt.Errorf("compile exclude_name regexp %q: %w", *excludeName, err)
		}
	}

	if excludeDb != nil {
		f.excludeDbRegexp, err = regexp.Compile(*excludeDb)
		if err != nil {
			return nil, fmt.Errorf("compile exclude_db regexp %q: %w", *excludeDb, err)
		}
	}

	return f, nil
}

// MatchName returns true if name matches the name regexp and does not match exclude.
func (f *Filter) MatchName(name string) bool {
	if f.excludeNameRegexp != nil && f.excludeNameRegexp.MatchString(name) {
		return false
	}

	return f.nameRegexp.MatchString(name)
}

// MatchDb returns true if database name matches the db regexp and does not match exclude.
func (f *Filter) MatchDb(name string) bool {
	if f.excludeDbRegexp != nil && f.excludeDbRegexp.MatchString(name) {
		return false
	}

	if f.dbRegexp == nil {
		return true
	}

	return f.dbRegexp.MatchString(name)
}
