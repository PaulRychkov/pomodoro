package plan

import "testing"

func durations(n, focus, brk int) []slotDuration {
	out := make([]slotDuration, n)
	for i := range out {
		out[i] = slotDuration{focus: focus, brk: brk}
	}
	return out
}

func TestStartsChainFromNowWhenIdle(t *testing.T) {
	got := projectStarts(durations(3, 25, 5), nil, 9*60, nil)
	want := []int{540, 570, 600}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("слот %d: начало %d, ожидалось %d (все: %v)", i, got[i], want[i], got)
		}
	}
}

func TestLateStartShiftsWholeTail(t *testing.T) {
	onTime := projectStarts(durations(3, 25, 5), nil, 9*60, nil)
	late := projectStarts(durations(3, 25, 5), nil, 9*60+5, nil)
	for i := range onTime {
		if late[i] != onTime[i]+5 {
			t.Fatalf("опоздание на 5 минут должно сдвинуть весь хвост: %v против %v", late, onTime)
		}
	}
}

func TestRunningFocusKeepsItsRealStart(t *testing.T) {
	active := &activeSlot{startMin: 9*60 + 7, remainingMin: 18, isFocus: true}
	got := projectStarts(durations(3, 25, 5), nil, 9*60+14, active)
	if got[0] != 9*60+7 {
		t.Fatalf("идущий помидор обязан показывать своё реальное начало, получено %d", got[0])
	}
	if got[1] != 9*60+14+18+5 {
		t.Fatalf("следующий слот считается от конца текущего и перерыва, получено %d", got[1])
	}
	if got[2] != got[1]+30 {
		t.Fatalf("дальше цепочка идёт ровно, получено %v", got)
	}
}

func TestLongerRestPushesNextSlots(t *testing.T) {
	short := projectStarts(durations(2, 25, 5), []int{540}, 9*60+30, nil)
	long := projectStarts(durations(2, 25, 5), []int{540}, 9*60+40, nil)
	if long[1] != short[1]+10 {
		t.Fatalf("десять лишних минут отдыха обязаны сдвинуть следующий помидор: %v против %v", long, short)
	}
}

func TestDoneSlotsKeepFactualTimes(t *testing.T) {
	got := projectStarts(durations(4, 25, 5), []int{500, 533}, 10*60, nil)
	if got[0] != 500 || got[1] != 533 {
		t.Fatalf("сделанные помидоры показывают фактическое время: %v", got)
	}
	if got[2] != 600 || got[3] != 630 {
		t.Fatalf("остальные считаются от текущего момента: %v", got)
	}
}

func TestBreakInProgressDelaysNextSlot(t *testing.T) {
	active := &activeSlot{remainingMin: 4}
	got := projectStarts(durations(2, 25, 5), []int{540}, 9*60+27, active)
	if got[1] != 9*60+27+4 {
		t.Fatalf("следующий помидор начинается после перерыва, получено %d", got[1])
	}
}

func TestDurationsPerSlotRespected(t *testing.T) {
	d := []slotDuration{{focus: 17, brk: 6}, {focus: 23, brk: 15}, {focus: 25, brk: 5}}
	got := projectStarts(d, nil, 6*60, nil)
	want := []int{360, 360 + 17 + 6, 360 + 17 + 6 + 23 + 15}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("слот %d: %d, ожидалось %d (все: %v)", i, got[i], want[i], got)
		}
	}
}
