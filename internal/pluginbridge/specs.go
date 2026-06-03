package pluginbridge

// ConfigSpecs extracts the config-declared plugin specifiers from a merged
// opencode config map. opencode's `plugin` config value is an array whose
// entries are either a bare identifier string or a [spec, options] tuple
// (opencode/packages/opencode/src/config/plugin.ts:12 Spec union). Only the
// identifier is bridged; inline options are passed by the host at load time.
//
// Local {plugin,plugins}/*.{ts,js} files are discovered by the host itself
// (config/plugin.ts:29-37), so they are not included here.
func ConfigSpecs(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["plugin"].([]any)
	if !ok {
		return nil
	}
	specs := make([]string, 0, len(raw))
	for _, entry := range raw {
		switch v := entry.(type) {
		case string:
			if v != "" {
				specs = append(specs, v)
			}
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok && s != "" {
					specs = append(specs, s)
				}
			}
		}
	}
	return specs
}
