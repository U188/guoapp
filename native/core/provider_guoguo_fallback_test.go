package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGuoguoMediaURLsHandlesProtocolRelativeAndSpreads(t *testing.T) {
	hosts := guoguoCDNHosts
	t.Cleanup(func() { guoguoCDNHosts = hosts })
	guoguoCDNHosts = []string{"a.test", "b.test", "c.test"}

	if got := guoguoMediaURLs("//other.cdn/a.jpg"); len(got) != 1 || got[0] != "https://other.cdn/a.jpg" {
		t.Fatalf("协议相对地址拼接错误: %v", got)
	}
	if got := guoguoMediaURLs("https://fixed.test/a.jpg"); len(got) != 1 || got[0] != "https://fixed.test/a.jpg" {
		t.Fatalf("绝对地址被改写: %v", got)
	}
	urls := guoguoMediaURLs("/media/1.mp4")
	if len(urls) != 3 {
		t.Fatalf("没有生成全部 CDN 候选: %v", urls)
	}
	seen := map[string]bool{}
	for _, address := range urls {
		if !strings.HasPrefix(address, "https://") || !strings.HasSuffix(address, "/media/1.mp4") {
			t.Fatalf("候选地址异常: %v", urls)
		}
		seen[address] = true
	}
	if len(seen) != 3 {
		t.Fatalf("候选地址重复: %v", urls)
	}
}

func TestGuoguoTokenRefreshRetriesSameHost(t *testing.T) {
	hosts, scheme := guoguoAPIHosts, guoguoScheme
	t.Cleanup(func() {
		guoguoAPIHosts, guoguoScheme = hosts, scheme
	})
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("token")
		mu.Lock()
		calls = append(calls, r.URL.Path+"|"+token)
		mu.Unlock()
		switch r.URL.Path {
		case "/front/cluser/c/user/mac/login":
			_, _ = w.Write([]byte(`{"code":"0","data":{"token":"fresh"}}`))
		case "/front/media/getMediaById":
			if token != "fresh" {
				_, _ = w.Write([]byte(`{"code":"6","msg":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":"0","data":{"video":{}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	guoguoScheme = "http"
	guoguoAPIHosts = []string{strings.TrimPrefix(server.URL, "http://")}

	manager := downloadTestManager(t)
	client := &guoguoClient{host: guoguoAPIHosts[0], token: "stale", loggedAt: time.Now()}
	result, err := client.call(context.Background(), manager.engine.downloader, "/front/media/getMediaById", map[string]any{})
	if err != nil {
		t.Fatalf("令牌刷新后仍然失败: %v", err)
	}
	if mapString(result, "code") != "0" {
		t.Fatalf("返回码异常: %v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"/front/media/getMediaById|stale",
		"/front/cluser/c/user/mac/login|",
		"/front/media/getMediaById|fresh",
	}
	if len(calls) != len(want) {
		t.Fatalf("请求序列不符: %v", calls)
	}
	for index, call := range want {
		if calls[index] != call {
			t.Fatalf("请求序列不符: %v", calls)
		}
	}
}
