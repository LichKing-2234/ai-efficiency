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

func ApplyClaudeProxyEnv(env map[string]string, cfg ClaudeEnv) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	delete(env, "ANTHROPIC_API_KEY")
	for k, v := range BuildClaudeEnv(cfg) {
		env[k] = v
	}
	return env
}
