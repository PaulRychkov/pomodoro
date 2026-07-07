package sound

import _ "embed"

//go:embed ding.wav
var defaultChime []byte

func DefaultChime() []byte {
	out := make([]byte, len(defaultChime))
	copy(out, defaultChime)
	return out
}
