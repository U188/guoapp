package core

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// hongguoSearchAPIFixture 生成 /api/search/result 的真实响应形状：
// searchList 每项是 {name, keyword, video_data{series_id,...}}。
func hongguoSearchAPIFixture(page, totalPages, total int, ids ...string) string {
	rows := []any{}
	for _, id := range ids {
		rows = append(rows, map[string]any{
			"name":    "合成短剧 " + id,
			"keyword": id,
			"video_data": map[string]any{
				"series_id": id, "series_title": "合成短剧 " + id,
				"series_cover": "https://p6-novel.byteimg.com/cover/" + id,
				"episode_cnt":  12, "episode_right_text": "全12集",
			},
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"searchList": rows, "totalCount": total, "pageNum": page,
		"totalPages": totalPages, "hasMore": page < totalPages,
	})
	return string(payload)
}

// TestHongguoSearchUsesJSONAPI 保证搜索走仍在线的 JSON 接口，
// 而不是已下线的 /search/<关键词> HTML 路由（只返回空壳页）。
func TestHongguoSearchUsesJSONAPI(t *testing.T) {
	var requestedPath, requestedQuery string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if strings.HasPrefix(request.URL.Path, "/search/") {
			return sourceFixtureResponse(request, 200, "<!doctype html><html><body>shell</body></html>"), nil
		}
		if request.URL.Path != hongguoSearchAPI {
			return sourceFixtureResponse(request, 404, "{}"), nil
		}
		requestedPath = request.URL.Path
		requestedQuery = request.URL.Query().Get("query")
		if request.URL.Query().Get("aid") != hongguoSearchAPIAid {
			t.Fatal("missing aid", request.URL)
		}
		return sourceFixtureResponse(request, 200, hongguoSearchAPIFixture(1, 21, 207, "101", "102")), nil
	})
	entry, err := d.fetchHongguoSearchPage(context.Background(), "王妈在顾家当保姆")
	if err != nil {
		t.Fatal(err)
	}
	if requestedPath != hongguoSearchAPI || requestedQuery != "王妈在顾家当保姆" {
		t.Fatal("wrong request", requestedPath, requestedQuery)
	}
	if len(entry.Dramas) != 2 || entry.Dramas[0].ID != "hongguo:101" || entry.Total != 207 || !entry.Limited {
		t.Fatalf("unexpected entry %+v", entry)
	}
}

// TestHongguoSearchRejectsInvalidJSON 校验接口返回错误体时不会静默成功。
func TestHongguoSearchRejectsInvalidJSON(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, 400, `{"code":"invalid_query","message":"Provide one non-empty search query of at most 30 characters."}`), nil
	})
	if _, err := d.fetchHongguoSearchPage(context.Background(), "短剧"); err == nil {
		t.Fatal("accepted invalid_query payload as a valid search result")
	}
}

// TestHongguoSearchKeywordTruncation 校验关键词按接口口径截断到 30 字。
func TestHongguoSearchKeywordTruncation(t *testing.T) {
	long := strings.Repeat("剧", 40)
	got, err := hongguoSearchKeyword(long)
	if err != nil {
		t.Fatal(err)
	}
	if runes := []rune(got); len(runes) != hongguoSearchMaxQueryRunes {
		t.Fatalf("keyword not truncated to %d runes, got %d", hongguoSearchMaxQueryRunes, len(runes))
	}
	if _, err := hongguoSearchKeyword("   "); err == nil {
		t.Fatal("accepted blank keyword")
	}
}
