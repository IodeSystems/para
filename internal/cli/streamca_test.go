package cli

// streamCAParityCases are the parity rows of stream CA: vault settings.
func streamCAParityCases() map[string]parityCase {
	return map[string]parityCase{
		"vault_config_list":  {`{}`, []string{"vault", "config", "list"}, true},
		"vault_config_get":   {`{"key":"egress.default"}`, []string{"vault", "config", "get", "egress.default"}, true},
		"vault_config_set":   {`{"key":"align.days","value":"14"}`, []string{"vault", "config", "set", "align.days", "14"}, false},
		"vault_config_unset": {`{"key":"align.days"}`, []string{"vault", "config", "unset", "align.days"}, false},
	}
}
