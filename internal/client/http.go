package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"golang.org/x/net/proxy"
)

var (
	systemDirectClient *http.Client
	systemProxyClient  *http.Client
	systemProxyURL     string
	clientLock         sync.RWMutex
	customProxyClients = make(map[string]*customProxyClientEntry)
)

const customProxyClientCacheLimit = 64

type customProxyClientEntry struct {
	client   *http.Client
	lastUsed time.Time
}

// GetHTTPClientSystemProxy returns a cached http.Client.
// - useProxy=false: bypass proxy
// - useProxy=true: use proxy settings from system/app settings (setting key: proxy_url)
func GetHTTPClientSystemProxy(useProxy bool) (*http.Client, error) {
	if useProxy {
		currentProxyURL, err := op.SettingGetString(model.SettingKeyProxyURL)
		if err != nil {
			return nil, err
		}
		if currentProxyURL == "" {
			return nil, fmt.Errorf("proxy url is empty")
		}

		clientLock.RLock()
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			clientLock.RUnlock()
			return systemProxyClient, nil
		}
		clientLock.RUnlock()

		clientLock.Lock()
		defer clientLock.Unlock()

		// Re-check after acquiring write lock.
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			return systemProxyClient, nil
		}

		client, err := newHTTPClientCustomProxy(currentProxyURL)
		if err != nil {
			return nil, err
		}
		if systemProxyClient != nil {
			systemProxyClient.CloseIdleConnections()
		}
		systemProxyClient = client
		systemProxyURL = currentProxyURL
		return systemProxyClient, nil
	}

	clientLock.RLock()
	if !useProxy && systemDirectClient != nil {
		clientLock.RUnlock()
		return systemDirectClient, nil
	}
	clientLock.RUnlock()

	clientLock.Lock()
	defer clientLock.Unlock()

	if systemDirectClient != nil {
		return systemDirectClient, nil
	}
	client, err := newHTTPClientNoProxy()
	if err != nil {
		return nil, err
	}
	systemDirectClient = client
	return systemDirectClient, nil
}

// GetHTTPClientCustomProxy returns a bounded cached client per proxy URL so
// transports, idle connections, and their cleanup goroutines can be reused.
// proxyURL supports: http, https, socks, socks5
func GetHTTPClientCustomProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy url is empty")
	}
	proxyURL = strings.TrimSpace(proxyURL)
	now := time.Now()
	clientLock.Lock()
	if entry := customProxyClients[proxyURL]; entry != nil && entry.client != nil {
		entry.lastUsed = now
		client := entry.client
		clientLock.Unlock()
		return client, nil
	}
	clientLock.Unlock()

	client, err := newHTTPClientCustomProxy(proxyURL)
	if err != nil {
		return nil, err
	}

	clientLock.Lock()
	defer clientLock.Unlock()
	if entry := customProxyClients[proxyURL]; entry != nil && entry.client != nil {
		client.CloseIdleConnections()
		entry.lastUsed = now
		return entry.client, nil
	}
	if len(customProxyClients) >= customProxyClientCacheLimit {
		evictOldestCustomProxyClientLocked()
	}
	customProxyClients[proxyURL] = &customProxyClientEntry{client: client, lastUsed: now}
	return client, nil
}

func evictOldestCustomProxyClientLocked() {
	oldestKey := ""
	var oldest time.Time
	for key, entry := range customProxyClients {
		if entry == nil || oldestKey == "" || entry.lastUsed.Before(oldest) {
			oldestKey = key
			if entry != nil {
				oldest = entry.lastUsed
			}
		}
	}
	if oldestKey == "" {
		return
	}
	if entry := customProxyClients[oldestKey]; entry != nil && entry.client != nil {
		entry.client.CloseIdleConnections()
	}
	delete(customProxyClients, oldestKey)
}

func clonedDefaultTransport() (*http.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	cloned := transport.Clone()
	return cloned, nil
}

func newHTTPClientNoProxy() (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}
	cloned.Proxy = nil
	return &http.Client{Transport: cloned}, nil
}

func newHTTPClientCustomProxy(proxyURLStr string) (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}

	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		cloned.Proxy = http.ProxyURL(proxyURL)
	case "socks", "socks5":
		socksDialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy: %w", err)
		}
		cloned.Proxy = nil
		cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			contextDialer, ok := socksDialer.(proxy.ContextDialer)
			if !ok {
				return nil, fmt.Errorf("socks proxy dialer does not support context cancellation")
			}
			return contextDialer.DialContext(ctx, network, addr)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return &http.Client{Transport: cloned}, nil
}
