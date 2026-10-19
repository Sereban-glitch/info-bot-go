package config

import "testing"

func TestEffectiveDostupSyncMinutes(t *testing.T) {
	for _, tc := range []struct {
		cfg  *Config
		want int
	}{{nil, 20}, {&Config{}, 20}, {&Config{DostupSyncMinutes: -1}, 20}, {&Config{DostupSyncMinutes: 7}, 7}} {
		if got := tc.cfg.EffectiveDostupSyncMinutes(); got != tc.want {
			t.Fatalf("interval = %d, want %d", got, tc.want)
		}
	}
}
