package model

type AuthConfig struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
	OIDCProviders        []OIDCProvider
}

type OIDCProvider struct {
	Name string
}

type OIDCLoginStartResult struct {
	AuthURL      string
	StateBinding string
}

type OIDCCallbackResult struct {
	Tokens        Tokens
	RedirectAfter string
}
