package plan

type slotDuration struct {
	focus int
	brk   int
}

type activeSlot struct {
	startMin     int
	remainingMin int
	isFocus      bool
}

func projectStarts(durations []slotDuration, past []int, nowMin int, active *activeSlot) []int {
	out := make([]int, len(durations))
	for i := range out {
		out[i] = -1
	}
	for i, p := range past {
		if i < len(out) {
			out[i] = p
		}
	}
	next := len(past)
	if next > len(out) {
		next = len(out)
	}
	cursor := nowMin
	if active != nil {
		if active.isFocus {
			if next < len(out) {
				out[next] = active.startMin
				cursor = nowMin + active.remainingMin + durations[next].brk
				next++
			}
		} else {
			cursor = nowMin + active.remainingMin
		}
	}
	for i := next; i < len(out); i++ {
		out[i] = cursor
		cursor += durations[i].focus + durations[i].brk
	}
	return out
}
