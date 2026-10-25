package planlife

import "github.com/dbulashev/dasha/internal/pathprobe"

// fates needs the final path first.
func (s *state) fates() {
	chosen := map[int]bool{}
	for _, id := range s.a.Final.Tree {
		chosen[id] = true
	}

	for id, pv := range s.paths {
		displaced, isDisplaced := s.first(id, pathprobe.KindDisplaced, "")
		removed, isRemoved := s.first(id, pathprobe.KindRemoved, "")
		rejected, isRejected := s.first(id, pathprobe.KindRejected, "")

		switch {
		case chosen[id]:
			pv.Fate = FateChosen
		case isDisplaced:
			s.lostTo(pv, FateDisplaced, displaced)
		case isRemoved:
			s.lostTo(pv, FateRemoved, removed)
		case isRejected:
			s.lostTo(pv, FateRejected, rejected)
		case s.has(id, pathprobe.KindSurvived, ""):
			pv.Fate = FateSurvived
		case s.has(id, pathprobe.KindAccepted, ""):
			switch {
			case !s.tr.Coverage.Displaced:
				pv.Fate = FateUnknown
				pv.FateReason = ReasonDisplacedNotCovered
			case !s.tr.Coverage.Removed:
				pv.Fate = FateUnknown
				pv.FateReason = ReasonRemovedNotCovered
			default:
				pv.Fate = FateSurvived
			}
		case len(pv.Events) == 0 && len(pv.Parents) > 0:
			pv.Fate = FateChildOnly
		default:
			pv.Fate = FateUnknown
			pv.FateReason = ReasonNoEvents
		}
	}
}

func (s *state) lostTo(pv *PathView, f Fate, ev pathprobe.Event) {
	pv.Fate = f
	pv.By = ev.ByID
	pv.BySeq = ev.BySeq
	pv.Cause = s.cause(ev)
}

// cause compares costs; gap_cost is not used: for rejected it is measured
// against the rel's cheapest path, and it is omitted when 0.
func (s *state) cause(lost pathprobe.Event) *Cause {
	c := &Cause{Code: CauseUnknown, Startup: lost.CostStartup} //nolint:exhaustruct

	if lost.ByID == 0 || lost.CostTotal == nil {
		return c
	}

	winner, ok := s.costEvent(lost.ByID, lost.BySeq)
	if !ok {
		return c
	}

	gap := *lost.CostTotal - *winner.CostTotal
	c.Gap = &gap
	c.WinnerStartup = winner.CostStartup

	switch {
	case gap > 0:
		c.Code = CauseCostlierTotal
	case c.Startup == nil || c.WinnerStartup == nil:
		// no startup costs to compare: unknown
	case *c.WinnerStartup < *c.Startup:
		c.Code = CauseLostOnStartup
	default:
		c.Code = CauseLostNotOnCost
	}

	return c
}

// costEvent: the event at seq, else any event of the path; a Path's costs do not change.
func (s *state) costEvent(id, seq int) (pathprobe.Event, bool) {
	var (
		found pathprobe.Event
		ok    bool
	)

	for _, ev := range s.events[id] {
		if ev.CostTotal == nil {
			continue
		}

		if ev.Seq == seq {
			return ev, true
		}

		if !ok {
			found, ok = ev, true
		}
	}

	return found, ok
}
