package services

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
)

var version = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return "0.0.0-unknown"
}()

// AuthProvider is an interface for authorization providers
type AuthProvider struct {
	url      string
	login    string
	password string
}

// NewAuthProvider creates a new AuthProvider
func NewAuthProvider(url, login, password string) *AuthProvider {
	return &AuthProvider{
		url:      url,
		login:    login,
		password: password,
	}
}

// IsAuthorized checks if the IP is allowed
func (a *AuthProvider) IsAllowed(ctx context.Context, ip string) (bool, error) {
	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf(a.url, ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return false, err
	}
	if a.login != "" && a.password != "" {
		req.SetBasicAuth(a.login, a.password)
	}
	req.Header.Set("User-Agent", "Docker-Registry-Proxy/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	ok := resp.StatusCode == http.StatusOK
	if !ok {
		return false, fmt.Errorf("%s", resp.Status)
	}
	return true, nil
}
