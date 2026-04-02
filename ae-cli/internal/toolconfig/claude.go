package toolconfig

type ClaudeEnv struct {
	BaseURL string
	Token   string
}

func BuildClaudeEnv(cfg ClaudeEnv) map[string]string {
	return map[string]string{
		"ANTHROPIC_BASE_URL":   cfg.BaseURL,
		"ANTHROPIC_AUTH_TOKEN": cfg.Token,
	}
}
