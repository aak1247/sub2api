package service

import "fmt"

// NewOAuthRefreshExecutorForAccount returns the platform-specific executor used
// by OAuthRefreshAPI. It keeps all refresh entrypoints on the same lock/key path.
func NewOAuthRefreshExecutorForAccount(
	account *Account,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	antigravityOAuthService *AntigravityOAuthService,
) (OAuthRefreshExecutor, error) {
	if account == nil {
		return nil, fmt.Errorf("account is nil")
	}
	if account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("cannot refresh non-OAuth account")
	}

	switch account.Platform {
	case PlatformAnthropic:
		if oauthService == nil {
			return nil, fmt.Errorf("anthropic OAuth service is not configured")
		}
		return NewClaudeTokenRefresher(oauthService), nil
	case PlatformOpenAI:
		if openaiOAuthService == nil {
			return nil, fmt.Errorf("OpenAI OAuth service is not configured")
		}
		return NewOpenAITokenRefresher(openaiOAuthService, nil), nil
	case PlatformGemini:
		if geminiOAuthService == nil {
			return nil, fmt.Errorf("Gemini OAuth service is not configured")
		}
		return NewGeminiTokenRefresher(geminiOAuthService), nil
	case PlatformAntigravity:
		if antigravityOAuthService == nil {
			return nil, fmt.Errorf("Antigravity OAuth service is not configured")
		}
		return NewAntigravityTokenRefresher(antigravityOAuthService), nil
	default:
		return nil, fmt.Errorf("unsupported OAuth platform: %s", account.Platform)
	}
}
