package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type dnsCacheEntry struct {
	addresses []string
	expires   time.Time
}

type safeDNSDialer struct {
	client    *http.Client
	dialer    net.Dialer
	mu        sync.Mutex
	cache     map[string]dnsCacheEntry
	inFlight  map[string]chan struct{}
	endpoints []string
}

func newSafeDNSDialer(transport *http.Transport) *safeDNSDialer {
	lookupTransport := transport.Clone()
	lookupTransport.TLSClientConfig = nil
	return &safeDNSDialer{
		client: &http.Client{Transport: lookupTransport, Timeout: 4 * time.Second},
		dialer: net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
		cache:  map[string]dnsCacheEntry{}, inFlight: map[string]chan struct{}{},
		endpoints: []string{"https://dns.alidns.com/resolve", "https://dns.google/resolve"},
	}
}

func protectedCDNHost(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, ".zdmhyg.cn") || strings.HasSuffix(host, ".lkkwip.cn")
}

func (resolver *safeDNSDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !protectedCDNHost(host) {
		return resolver.dialer.DialContext(ctx, network, address)
	}
	addresses, err := resolver.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%s 安全 DNS 解析失败，可在网络设置中启用可用代理: %w", host, err)
	}
	var lastErr error
	for _, resolved := range addresses {
		connection, err := resolver.dialer.DialContext(ctx, network, net.JoinHostPort(resolved, port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%s 的 CDN 无法直连，请检查代理配置: %w", host, lastErr)
}

func (resolver *safeDNSDialer) lookup(ctx context.Context, host string) ([]string, error) {
	entry, err := resolver.lookupEntry(ctx, host, "")
	return entry.addresses, err
}

func (resolver *safeDNSDialer) lookupEntry(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	cacheKey := host + "|" + subnet
	for {
		resolver.mu.Lock()
		if entry, found := resolver.cache[cacheKey]; found && time.Now().Before(entry.expires) {
			resolver.mu.Unlock()
			return entry, nil
		}
		if pending := resolver.inFlight[cacheKey]; pending != nil {
			resolver.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return dnsCacheEntry{}, ctx.Err()
			}
		}
		resolver.inFlight[cacheKey] = make(chan struct{})
		resolver.mu.Unlock()
		break
	}
	entry, err := resolver.query(ctx, host, subnet)
	resolver.mu.Lock()
	if err == nil {
		resolver.cache[cacheKey] = entry
	}
	close(resolver.inFlight[cacheKey])
	delete(resolver.inFlight, cacheKey)
	resolver.mu.Unlock()
	return entry, err
}

func (resolver *safeDNSDialer) query(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	var lastErr error
	for _, endpoint := range resolver.endpoints {
		lookupURL, err := url.Parse(endpoint)
		if err != nil {
			return dnsCacheEntry{}, err
		}
		query := lookupURL.Query()
		query.Set("name", host)
		query.Set("type", "A")
		if subnet != "" {
			query.Set("edns_client_subnet", subnet)
		}
		lookupURL.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL.String(), nil)
		if err != nil {
			return dnsCacheEntry{}, err
		}
		request.Header.Set("Accept", "application/dns-json")
		response, err := resolver.client.Do(request)
		if err != nil {
			lastErr = publicError(err)
			continue
		}
		var payload struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				TTL  int    `json:"TTL"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&payload)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || err != nil || payload.Status != 0 {
			lastErr = errors.New("DoH 服务没有返回有效 DNS 记录")
			continue
		}
		entry := dnsCacheEntry{}
		ttl := 300
		for _, answer := range payload.Answer {
			address := net.ParseIP(answer.Data)
			if answer.Type != 1 || address == nil || address.To4() == nil || !address.IsGlobalUnicast() || address.IsPrivate() {
				continue
			}
			entry.addresses = append(entry.addresses, address.String())
			if answer.TTL < ttl {
				ttl = answer.TTL
			}
		}
		if len(entry.addresses) > 0 {
			if ttl < 1 {
				ttl = 1
			}
			entry.expires = time.Now().Add(time.Duration(ttl) * time.Second)
			return entry, nil
		}
		lastErr = errors.New("DoH 未返回公网 IPv4 地址")
	}
	return dnsCacheEntry{}, lastErr
}
