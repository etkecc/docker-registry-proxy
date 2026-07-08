package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/etkecc/go-kit"
	"github.com/etkecc/go-kit/httpclient"
	"github.com/hashicorp/golang-lru/v2/expirable"
)

var userAgent = sync.OnceValue(func() string { return kit.UserAgent("Docker-Registry-Proxy", "") })()

// AuthProvider is an interface for authorization providers
type AuthProvider struct {
	url          string
	login        string
	password     string
	cacheAllowed *expirable.LRU[string, bool]
	hc           *http.Client
}

// NewAuthProvider creates a new AuthProvider
func NewAuthProvider(url, login, password string) *AuthProvider {
	return &AuthProvider{
		url:          url,
		login:        login,
		password:     password,
		cacheAllowed: expirable.NewLRU[string, bool](1000, nil, 2*time.Hour),
		hc:           httpclient.NewSingleHost(),
	}
}

// IsAuthorized checks if the IP is allowed
func (a *AuthProvider) IsAllowed(ctx context.Context, ip string) (bool, error) {
	if cached, ok := a.cacheAllowed.Get(ip); ok {
		return cached, nil
	}

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
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.hc.Do(req)
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

// LoginVia checks if the IP is allowed
func (a *AuthProvider) LoginVia(ctx context.Context, ip, domain, via string) error {
	if cached, ok := a.cacheAllowed.Get(ip); ok {
		a.cacheAllowed.Add(ip, cached)
		return nil
	}

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf(a.url, domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	if a.login != "" && a.password != "" {
		req.SetBasicAuth(a.login, a.password)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ok := resp.StatusCode == http.StatusOK
	if !ok {
		return nil
	}

	type respIem struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	var result []*respIem
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		return nil
	}
	hash := kit.Hash(result[0].Labels["domain"] + result[0].Labels["subscription_provider"] + result[0].Labels["order_issue_id"])
	if hash == via {
		a.cacheAllowed.Add(ip, true)
	}
	return nil
}
