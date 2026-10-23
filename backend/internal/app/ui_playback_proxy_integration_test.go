package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlaybackNginxFlushesMediaAndPreservesCookies(t *testing.T) {
	nginx, err := exec.LookPath("nginx")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("local Nginx is unavailable")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ready" {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		cookie, err := request.Cookie("synthetic_viewer")
		if err != nil || cookie.Value != "local-fixture" {
			http.Error(writer, "missing fixture identity", http.StatusUnauthorized)
			return
		}
		if request.URL.Path == "/stream" && !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		http.SetCookie(writer, &http.Cookie{Name: "synthetic_seen", Value: "yes", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "synthetic-init")
		writer.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	directory := t.TempDir()
	configuration := fmt.Sprintf(`worker_processes 1;
error_log stderr;
pid "%[1]s/nginx.pid";
lock_file "%[1]s/nginx.lock";
events { worker_connections 32; }
http {
    access_log off;
    client_body_temp_path "%[1]s/client_temp";
    proxy_temp_path "%[1]s/proxy_temp";
    fastcgi_temp_path "%[1]s/fastcgi_temp";
    uwsgi_temp_path "%[1]s/uwsgi_temp";
    scgi_temp_path "%[1]s/scgi_temp";
    server {
        listen %[2]s;
        location / {
            proxy_pass %[3]s;
            proxy_set_header Host $http_host;
            proxy_http_version 1.1;
            proxy_buffering on;
            proxy_buffer_size 4k;
            proxy_buffers 8 4k;
        }
    }
}
`, directory, address, upstream.URL)
	path := filepath.Join(directory, "nginx.conf")
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, nginx, "-e", "stderr", "-p", directory+string(os.PathSeparator), "-c", path, "-g", "daemon off; master_process off;")
	output := &cappedStringWriter{limit: 8192}
	command.Stderr = output
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	defer func() { cancel(); <-done }()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	base := "http://" + address
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, err := client.Get(base + "/ready")
		if err == nil {
			response.Body.Close()
			break
		}
		select {
		case <-done:
			if strings.Contains(output.String(), "sysctlbyname") && strings.Contains(output.String(), "Operation not permitted") {
				t.Skip("local Nginx cannot read TCP settings in the sandbox; run this integration test with system access")
			}
			t.Fatal("Nginx fixture exited", exitErr, output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Nginx fixture did not start", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, route := range []string{"buffered", "stream"} {
		t.Run(route, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodGet, base+"/"+route, nil)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set("Origin", "https://library.example.invalid")
			request.AddCookie(&http.Cookie{Name: "synthetic_viewer", Value: "local-fixture"})
			started := time.Now()
			response, err := client.Do(request)
			var body []byte
			if err == nil {
				defer response.Body.Close()
				body = make([]byte, len("synthetic-init"))
				_, err = io.ReadFull(response.Body, body)
			}
			if route == "buffered" {
				if err == nil {
					t.Logf("default buffering also forwarded this fixture in %s; buffering alone does not establish a deployment failure", time.Since(started))
				}
				return
			}
			if err != nil || string(body) != "synthetic-init" || response.StatusCode != http.StatusOK {
				t.Fatal("Nginx buffered or rejected media initialization", err, string(body))
			}
			if !strings.Contains(response.Header.Get("Cache-Control"), "no-store") || !strings.Contains(response.Header.Get("Set-Cookie"), "synthetic_seen=yes") {
				t.Fatal("proxy lost privacy or browser identity headers", response.Header)
			}
			t.Logf("first media bytes arrived through Nginx in %s before upstream completion", time.Since(started))
		})
	}
}
