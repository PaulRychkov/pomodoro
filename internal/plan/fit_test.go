package plan

import "testing"

func cfg() FitConfig { return DefaultFitConfig(25, 5, 15, 3) }

func TestFitFortyMinutesGivesTwoPomodoros(t *testing.T) {
	f := FitPeriod(40, cfg())
	if f.Count != 2 {
		t.Fatalf("в 40 минут должно лечь 2 помидора, получено %d (%+v)", f.Count, f)
	}
	if f.Short < 5 {
		t.Fatalf("перерыв урезан до %d, минимум 5", f.Short)
	}
	if used := cfg().span(f.Count, f.Focus, f.Short, f.Long); used+f.Slack != 40 {
		t.Fatalf("период не сходится: занято %d + остаток %d != 40", used, f.Slack)
	}
}

func TestFitNeverShortensBreaks(t *testing.T) {
	c := cfg()
	for _, w := range []int{35, 40, 55, 60, 90, 100, 180, 240} {
		f := FitPeriod(w, c)
		if f.Count == 0 {
			continue
		}
		if f.Short < c.MinShort {
			t.Fatalf("окно %d: короткий перерыв %d меньше минимума %d", w, f.Short, c.MinShort)
		}
		_, longs := breakCounts(f.Count, c.BlockSize)
		if longs > 0 && f.Long < c.MinLong {
			t.Fatalf("окно %d: длинный перерыв %d меньше минимума %d", w, f.Long, c.MinLong)
		}
		if used := c.span(f.Count, f.Focus, f.Short, f.Long); used > w {
			t.Fatalf("окно %d: занято %d — вылезли за границу", w, used)
		}
	}
}

func TestFitFourHourWindow(t *testing.T) {
	f := FitPeriod(240, cfg())
	if f.Count < 7 {
		t.Fatalf("в 4 часа должно лечь не меньше 7 помидоров, получено %d (%+v)", f.Count, f)
	}
	if f.Slack > cfg().MaxSlack {
		t.Fatalf("простой %d минут слишком велик: %+v", f.Slack, f)
	}
}

func TestFitRespectsBlockSize(t *testing.T) {
	c := DefaultFitConfig(25, 5, 15, 4)
	f := FitPeriod(240, c)
	_, longs := breakCounts(f.Count, 4)
	if f.Count >= 5 && longs == 0 {
		t.Fatalf("при блоке 4 и %d помидорах должен быть хотя бы один длинный перерыв", f.Count)
	}
}

func TestFitTinyWindowGivesNothing(t *testing.T) {
	if f := FitPeriod(10, cfg()); f.Count != 0 {
		t.Fatalf("в 10 минут помидор не влезает, получено %+v", f)
	}
}
