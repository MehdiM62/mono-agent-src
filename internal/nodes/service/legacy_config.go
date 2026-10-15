package service

// Helpers for the canonical nodes that also run legacy comm.* configs
// (comm.bluesky, comm.mastodon, comm.reddit, comm.discord,
// comm.outlook_read, comm.outlook_send are registry aliases of service.*).
// They translate an old config shape into the canonical one without
// mutating the caller's map.

// withDefault returns a copy of config with key set to value.
func withDefault(config map[string]interface{}, key string, value interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config)+1)
	for k, v := range config {
		out[k] = v
	}
	out[key] = value
	return out
}

// withFallback returns config with key set from the first non-empty string
// among fallbacks when key itself is empty; otherwise config unchanged.
func withFallback(config map[string]interface{}, key string, fallbacks ...string) map[string]interface{} {
	if strVal(config, key) != "" {
		return config
	}
	for _, fb := range fallbacks {
		if v := strVal(config, fb); v != "" {
			return withDefault(config, key, v)
		}
	}
	return config
}
