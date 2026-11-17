package model

type AuthConfig struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
	OIDCProviders        []OIDCProvider
}

// OIDCProvider is an enabled external provider; Available is false while SSO has not reached it.
type OIDCProvider struct {
	Name      string
	Available bool
}

type OIDCLoginStartResult struct {
	AuthURL      string
	StateBinding string
}

type OIDCCallbackResult struct {
	Tokens        Tokens
	RedirectAfter string
}
