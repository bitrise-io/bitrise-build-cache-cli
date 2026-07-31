package doctor

// EffectiveState is an item's state once an applied fix is taken into account.
func EffectiveState(it ReportItem) State {
	if it.FixResult != nil {
		return StateOK
	}

	return it.Result.State
}

func EffectiveOverall(r Report) State {
	worst := StateOK
	for _, it := range r.Items {
		switch EffectiveState(it) {
		case StateError:
			return StateError
		case StateWarn:
			worst = StateWarn
		case StateOK:
		}
	}

	return worst
}

func ItemDisplay(it ReportItem) (State, string) {
	if it.FixResult != nil {
		return StateOK, "fixed: " + *it.FixResult
	}

	return it.Result.State, it.Result.Detail
}

// Partition splits items into not-OK and OK, preserving check order.
func Partition(items []ReportItem) (issues, healthy []ReportItem) { //nolint:nonamedreturns // two same-typed returns benefit from labels
	for _, it := range items {
		if EffectiveState(it) == StateOK {
			healthy = append(healthy, it)
		} else {
			issues = append(issues, it)
		}
	}

	return issues, healthy
}
