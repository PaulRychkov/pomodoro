package syncer

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

func TestSettingsPayloadCarriesUpdatedAt(t *testing.T) {
	s := models.DefaultSettings()
	s.WindowSharePercent = 34
	s.UpdatedAt = time.Date(2026, 7, 29, 12, 30, 0, 0, time.UTC)

	raw, err := json.Marshal(Changes{Settings: []SettingsPayload{{Settings: s, UpdatedAt: s.UpdatedAt}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back Changes
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Settings) != 1 {
		t.Fatalf("настройки потеряны при передаче: %s", raw)
	}
	got := back.Settings[0].settings()
	if !got.UpdatedAt.Equal(s.UpdatedAt) {
		t.Fatalf("updated_at потерян: got %v want %v (%s)", got.UpdatedAt, s.UpdatedAt, raw)
	}
	if got.WindowSharePercent != 34 {
		t.Fatalf("window_share_percent потерян: %+v", got)
	}
	if got.ID != 1 {
		t.Fatalf("id настроек должен быть 1, получено %d", got.ID)
	}
}
