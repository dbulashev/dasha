package querystats

import "github.com/dbulashev/dasha/internal/dto"

// Key identifies a report row across sources. A queryid is derived from the
// parse tree, object OIDs included, so it is unique only within a database:
// pairing two snapshots by queryid alone would merge unrelated statements.
type Key struct {
	QueryID int64
	Datname string
}

func KeyOf(r dto.QueryReport) Key {
	return Key{QueryID: r.QueryID, Datname: r.Datname}
}

// ResolveScope decides which slice of the instance an answer covers. Without a
// named database only the instance-wide reading is possible, which is also what
// callers written before the parameter existed expect.
func ResolveScope(scope *string, database string) string {
	if database == "" || (scope != nil && *scope == dto.ScopeInstance) {
		return dto.ScopeInstance
	}

	return dto.ScopeDatabase
}

// compareScope decides how two sides are read together, or reports false when
// they cannot be. Mixing generations would not merely lose rows: with the
// database part of the key empty on one side only, nothing matches and every
// statement is reported as both removed and added — worse than a refusal.
func compareScope(attributedA, attributedB bool, scope *string, database string) (string, bool) {
	if attributedA != attributedB {
		return "", false
	}

	if !attributedA {
		return dto.ScopeInstance, true
	}

	return ResolveScope(scope, database), true
}

// ScopeReport narrows an instance-wide report to the requested scope and moves
// the matching share set into the *Pct fields the contract exposes. The
// repository always reads every database — a stored snapshot has to stay usable
// after the user switches database — so this is the only place the narrowing
// happens.
func ScopeReport(rows []dto.QueryReport, scope, database string) []dto.QueryReport {
	out := make([]dto.QueryReport, 0, len(rows))

	for _, r := range rows {
		if scope == dto.ScopeInstance {
			r.RowsPct = r.RowsPctInstance
			r.CallsPct = r.CallsPctInstance
			r.TotalTimePct = r.TotalTimePctInstance
			r.IoTimePct = r.IoTimePctInstance
			r.CpuTimePct = r.CpuTimePctInstance
			r.SharedBlksDirtiedPct = r.SharedBlksDirtiedPctInstance
			r.SharedBlksWrittenPct = r.SharedBlksWrittenPctInstance
			r.WalBytesPct = r.WalBytesPctInstance
			r.TempBlksPct = r.TempBlksPctInstance
		} else if r.Datname != database {
			continue
		}

		out = append(out, r)
	}

	return out
}

// excludeReportsByUser drops reports run solely by the excluded users. Snapshot
// stats are pre-aggregated, so a query shared with a non-excluded user can't be
// trimmed per user and is kept as-is (matching the live side, which retains it).
func excludeReportsByUser(reports []dto.QueryReport, exclude []string) []dto.QueryReport {
	if len(exclude) == 0 {
		return reports
	}

	excluded := make(map[string]struct{}, len(exclude))
	for _, u := range exclude {
		excluded[u] = struct{}{}
	}

	out := make([]dto.QueryReport, 0, len(reports))

	for _, r := range reports {
		if onlyExcludedUsers(r.Usernames, excluded) {
			continue
		}

		out = append(out, r)
	}

	return out
}

// onlyExcludedUsers is true when every user that ran the query is excluded;
// unknown attribution (no usernames) is kept.
func onlyExcludedUsers(usernames []string, excluded map[string]struct{}) bool {
	if len(usernames) == 0 {
		return false
	}

	for _, u := range usernames {
		if _, ok := excluded[u]; !ok {
			return false
		}
	}

	return true
}
