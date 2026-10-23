package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

type systemProxy struct {
	http   string
	https  string
	bypass []string
}

type proxyRouter struct {
	mu          sync.RWMutex
	resolve     func(*http.Request) (*url.URL, error)
	description string
}

func (router *proxyRouter) configure(raw string) {
	resolve, description := networkProxy(raw)
	router.mu.Lock()
	router.resolve = resolve
	router.description = description
	router.mu.Unlock()
}

func (router *proxyRouter) proxy(request *http.Request) (*url.URL, error) {
	router.mu.RLock()
	resolve := router.resolve
	router.mu.RUnlock()
	if resolve == nil {
		return nil, nil
	}
	return resolve(request)
}

func (router *proxyRouter) summary() string {
	router.mu.RLock()
	defer router.mu.RUnlock()
	return router.description
}

func publicProxyConfig(raw string) (string, string, bool) {
	if raw == "" || raw == "auto" {
		return "auto", "", false
	}
	if raw == "direct" {
		return "direct", "", false
	}
	endpoint, err := url.Parse(raw)
	if err != nil {
		return "manual", "", false
	}
	hasAuth := endpoint.User != nil
	endpoint.User = nil
	return "manual", endpoint.String(), hasAuth
}

func configuredProxy(raw string) (*url.URL, error) {
	if raw == "" || raw == "auto" || raw == "direct" {
		return nil, nil
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" && endpoint.Scheme != "socks5" && endpoint.Scheme != "socks5h" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return nil, errors.New("proxyURL 必须是 auto、direct 或 HTTP/HTTPS/SOCKS5 代理地址")
	}
	return endpoint, nil
}

func networkProxy(raw string) (func(*http.Request) (*url.URL, error), string) {
	if raw == "direct" {
		return nil, "直连（手动设置）"
	}
	if endpoint, err := configuredProxy(raw); err != nil {
		return func(*http.Request) (*url.URL, error) { return nil, err }, "代理配置无效"
	} else if endpoint != nil {
		return func(request *http.Request) (*url.URL, error) {
			if proxyBypassed(request.URL.Hostname(), nil) {
				return nil, nil
			}
			return endpoint, nil
		}, "手动代理 " + endpoint.Scheme + "://" + endpoint.Host
	}
	system := discoverSystemProxy()
	description := "直连（未发现代理）"
	if system.http != "" || system.https != "" {
		description = "系统代理 " + firstNonEmpty(system.https, system.http)
	}
	if firstNonEmpty(os.Getenv("HTTPS_PROXY"), os.Getenv("https_proxy"), os.Getenv("HTTP_PROXY"), os.Getenv("http_proxy")) != "" {
		description = "环境变量代理"
	}
	return func(request *http.Request) (*url.URL, error) {
		if proxyBypassed(request.URL.Hostname(), system.bypass) {
			return nil, nil
		}
		environment := "HTTP_PROXY"
		remote := system.http
		if request.URL.Scheme == "https" {
			environment = "HTTPS_PROXY"
			remote = system.https
		}
		if firstNonEmpty(os.Getenv(environment), os.Getenv(strings.ToLower(environment))) != "" {
			return http.ProxyFromEnvironment(request)
		}
		if proxyBypassed(request.URL.Hostname(), strings.Split(firstNonEmpty(os.Getenv("NO_PROXY"), os.Getenv("no_proxy")), ",")) || remote == "" {
			return nil, nil
		}
		return url.Parse(remote)
	}, description
}

func proxyBypassed(host string, exceptions []string) bool {
	host = strings.ToLower(host)
	address := net.ParseIP(host)
	if host == "localhost" || address != nil && address.IsLoopback() {
		return true
	}
	for _, entry := range exceptions {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "*" || entry == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && address != nil && network.Contains(address) {
			return true
		}
		if strings.HasPrefix(entry, "*.") || strings.HasPrefix(entry, ".") {
			suffix := strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if entry != "" && host == entry {
			return true
		}
	}
	return false
}

func discoverSystemProxy() systemProxy {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		body, err := exec.CommandContext(ctx, "scutil", "--proxy").Output()
		if err == nil {
			return parseMacSystemProxy(string(body))
		}
	case "windows":
		body, err := exec.CommandContext(ctx, "reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`).Output()
		if err == nil {
			return parseWindowsSystemProxy(string(body))
		}
	}
	return systemProxy{}
}

func parseMacSystemProxy(raw string) systemProxy {
	values := map[string]string{}
	result := systemProxy{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " : ")
		if !ok {
			continue
		}
		values[key] = value
		if key != "" && key[0] >= '0' && key[0] <= '9' {
			result.bypass = append(result.bypass, value)
		}
	}
	for _, scheme := range []string{"HTTP", "HTTPS"} {
		if values[scheme+"Enable"] != "1" || values[scheme+"Proxy"] == "" || values[scheme+"Port"] == "" {
			continue
		}
		endpoint := "http://" + net.JoinHostPort(values[scheme+"Proxy"], values[scheme+"Port"])
		if scheme == "HTTP" {
			result.http = endpoint
		} else {
			result.https = endpoint
		}
	}
	return result
}

func parseWindowsSystemProxy(raw string) systemProxy {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			values[parts[0]] = strings.Join(parts[2:], " ")
		}
	}
	if values["ProxyEnable"] != "0x1" {
		return systemProxy{}
	}
	result := systemProxy{bypass: strings.Split(values["ProxyOverride"], ";")}
	for _, entry := range strings.Split(values["ProxyServer"], ";") {
		scheme, endpoint, assigned := strings.Cut(entry, "=")
		if !assigned {
			endpoint = entry
		}
		if endpoint == "" {
			continue
		}
		if !strings.Contains(endpoint, "://") {
			endpoint = "http://" + endpoint
		}
		if !assigned || scheme == "http" {
			result.http = endpoint
		}
		if !assigned || scheme == "https" {
			result.https = endpoint
		}
	}
	return result
}
