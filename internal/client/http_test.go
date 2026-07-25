package client

import "testing"

func TestCustomProxyClientIsReused(t *testing.T) {
	const proxyURL = "http://127.0.0.1:18080"
	clientLock.Lock()
	delete(customProxyClients, proxyURL)
	clientLock.Unlock()
	t.Cleanup(func() {
		clientLock.Lock()
		if entry := customProxyClients[proxyURL]; entry != nil && entry.client != nil {
			entry.client.CloseIdleConnections()
		}
		delete(customProxyClients, proxyURL)
		clientLock.Unlock()
	})

	first, err := GetHTTPClientCustomProxy(proxyURL)
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, err := GetHTTPClientCustomProxy(proxyURL)
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("expected custom proxy client to be reused")
	}
}
