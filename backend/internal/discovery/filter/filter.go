// Package filter implements regexp matching for cluster and database names.
package filter

import "regexp"

// Filter implements regexp filters for clusters and their databases.
type Filter struct {
	nameRegexp        *regexp.Regexp
	dbRegexp          *regexp.Regexp
	excludeNameRegexp *regexp.Regexp
	excludeDbRegexp   *regexp.Regexp
}

// New returns a Filter with compiled regexps.
func New(name string, db, excludeName, excludeDb *string) *Filter {
	f := &Filter{} //nolint:exhaustruct

	f.nameRegexp = regexp.MustCompile(name)
	if db != nil {
		f.dbRegexp = regexp.MustCompile(*db)
	}

	if excludeName != nil {
		f.excludeNameRegexp = regexp.MustCompile(*excludeName)
	}

	if excludeDb != nil {
		f.excludeDbRegexp = regexp.MustCompile(*excludeDb)
	}

	return f
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
