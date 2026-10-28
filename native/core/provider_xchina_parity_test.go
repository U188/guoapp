package core

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestXChinaSourceRegistrationCoversDispatchPoints 防止新增站源时漏接分发点。
func TestXChinaSourceRegistrationCoversDispatchPoints(t *testing.T) {
	if canonicalProviderSource("xchina") != sourceXChina || canonicalProviderSource("xchina.tube") != sourceXChina {
		t.Fatal("xchina canonical source missing")
	}
	if !isHuangguoProviderSource(sourceXChina) {
		t.Fatal("xchina source not registered in provider whitelist")
	}
	if got := (&Downloader{cfg: defaultConfig()}).providerBaseURL(sourceXChina); got != "https://xchina.tube" {
		t.Fatalf("unexpected xchina endpoint: %s", got)
	}
	for _, address := range []string{"https://xchina.tube/", "https://www.xchina.tube/video/abc123"} {
		if got := providerSourceForURL(address); got != sourceXChina {
			t.Fatalf("xchina domain not recognized: %s -> %s", address, got)
		}
	}
	if !validNativeCategory(sourceXChina, "recent") || validNativeCategory(sourceXChina, "") {
		t.Fatal("xchina category gate broken")
	}
	if len(catalogSortOptions(sourceXChina)) == 0 || len(catalogSortOptions(sourceHuangju)) == 0 {
		t.Fatal("catalog sort options missing")
	}
	found := false
	for _, board := range rankingBoards {
		if board.Source == sourceXChina {
			found = true
		}
	}
	if !found {
		t.Fatal("xchina ranking board missing")
	}
	if _, ok := findRankingBoard("xchina-hot"); !ok {
		t.Fatal("xchina-hot board missing")
	}
}

// TestXChinaCatalogSearchUsesKeywordsRoute 校验搜索走 /searchSevenVideos 且带必需筛选字段。
func TestXChinaCatalogSearchUsesKeywordsRoute(t *testing.T) {
	var sawSearch bool
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		body, err := readFixtureBody(request)
		if err != nil {
			t.Fatal(err)
		}
		route := fixtureRoute(body)
		if route != "/searchSevenVideos" {
			t.Fatalf("search did not use json api: %s", body)
		}
		sawSearch = true
		for _, key := range []string{"filterCategory", "filterTimeInDays", "filterViews", "filterDurationInMins", "sortBy", "keywords"} {
			if !strings.Contains(body, key) {
				t.Fatalf("search payload missing %s: %s", key, body)
			}
		}
		plain := `{"list":[{"id":"kw1","title":"搜索结果"}]}`
		return sourceFixtureResponse(request, 200, xchinaSeal(t, []byte(plain))), nil
	})
	items, _, err := d.fetchXChinaCatalogPage(context.Background(), 1, "", "测试")
	if err != nil || !sawSearch || len(items) != 1 || items[0].SourceID != "kw1" {
		t.Fatalf("xchina search failed: items=%d saw=%t err=%v", len(items), sawSearch, err)
	}
}

func readFixtureBody(request *http.Request) (string, error) {
	if request.Body == nil {
		return "", nil
	}
	defer request.Body.Close()
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 1024)
	for {
		n, err := request.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
		if len(buf) > 1<<20 {
			break
		}
	}
	return string(buf), nil
}

// fixtureRoute 从业务信封里取出站点路由，供测试断言请求形状。
func fixtureRoute(body string) string {
	marker := `"url":`
	index := strings.Index(body, marker)
	if index < 0 {
		return ""
	}
	rest := body[index+len(marker):]
	rest = strings.TrimLeft(rest, " ")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	end := strings.Index(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}
