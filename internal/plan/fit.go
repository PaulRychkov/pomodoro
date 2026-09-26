package plan

type FitConfig struct {
	Focus      int
	Short      int
	Long       int
	BlockSize  int
	MinFocus   int
	MaxFocus   int
	MinShort   int
	MinLong    int
	MaxSlack   int
}

type Fit struct {
	Count int
	Focus int
	Short int
	Long  int
	Slack int
}

func DefaultFitConfig(focus, short, long, blockSize int) FitConfig {
	if focus <= 0 {
		focus = 25
	}
	if short <= 0 {
		short = 5
	}
	if long <= 0 {
		long = 15
	}
	if blockSize <= 0 {
		blockSize = 3
	}
	minFocus := focus * 2 / 3
	if minFocus < 1 {
		minFocus = 1
	}
	return FitConfig{
		Focus:     focus,
		Short:     short,
		Long:      long,
		BlockSize: blockSize,
		MinFocus:  minFocus,
		MaxFocus:  focus,
		MinShort:  short,
		MinLong:   long,
		MaxSlack:  4,
	}
}

func breakCounts(count, blockSize int) (shorts, longs int) {
	if count <= 1 {
		return 0, 0
	}
	for i := 1; i < count; i++ {
		if i%blockSize == 0 {
			longs++
		} else {
			shorts++
		}
	}
	return shorts, longs
}

func (c FitConfig) span(count, focus, short, long int) int {
	shorts, longs := breakCounts(count, c.BlockSize)
	return count*focus + shorts*short + longs*long
}

// FitPeriod подбирает, сколько помидоров и какой длины уложить в период window.
// Перерывы никогда не короче настроенных: лишние минуты уходят в отдых, а не
// в работу. Длина фокуса ужимается только до MinFocus.
func FitPeriod(window int, c FitConfig) Fit {
	best := Fit{}
	if window <= 0 {
		return best
	}
	for count := 1; count <= window/c.MinFocus+1; count++ {
		if c.span(count, c.MinFocus, c.MinShort, c.MinLong) > window {
			break
		}
		for focus := c.MaxFocus; focus >= c.MinFocus; focus-- {
			used := c.span(count, focus, c.MinShort, c.MinLong)
			if used > window {
				continue
			}
			short, long := c.MinShort, c.MinLong
			shorts, longs := breakCounts(count, c.BlockSize)
			slack := window - used
			if longs > 0 && slack > 0 {
				add := slack / longs
				long += add
				slack -= add * longs
			}
			if shorts > 0 && slack > 0 {
				add := slack / shorts
				short += add
				slack -= add * shorts
			}
			cand := Fit{Count: count, Focus: focus, Short: short, Long: long, Slack: slack}
			if better(cand, best) {
				best = cand
			}
			break
		}
	}
	return best
}

// Лучше тот вариант, что даёт больше чистого фокуса. При равенстве —
// с более длинными помидорами: дробить работу без нужды незачем.
func better(a, b Fit) bool {
	if b.Count == 0 {
		return a.Count > 0
	}
	af, bf := a.Count*a.Focus, b.Count*b.Focus
	if af != bf {
		return af > bf
	}
	if a.Focus != b.Focus {
		return a.Focus > b.Focus
	}
	return a.Slack < b.Slack
}
