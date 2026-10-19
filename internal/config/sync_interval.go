package config

// EffectiveDostupSyncMinutes is shared by the worker and its status display.
func (c *Config) EffectiveDostupSyncMinutes() int {
	if c != nil && c.DostupSyncMinutes > 0 {
		return c.DostupSyncMinutes
	}
	return 20
}
