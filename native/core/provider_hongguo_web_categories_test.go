package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func hongguoWebCategoryFixture(route string, page, totalPages int, ids ...string) string {
	rows := []any{}
	for _, id := range ids {
		rows = append(rows, map[string]any{"series_id": id, "series_name": "合成分类 " + id, "episode_cnt": 12})
	}
	data, _ := json.Marshal(map[string]any{"loaderData": map[string]any{"category_$": map[string]any{
		"isSuccess": true, "categoryRoute": map[string]any{"contentType": route},
		"canonicalUrl":  "https://hongguoduanju.com/category/" + route,
		"recommendList": rows, "pagination": map[string]any{"pageNum": page, "pageSize": 24, "totalPages": totalPages},
	}}})
	return "<script>window._ROUTER_DATA=" + string(data) + ";</script>"
}

func TestHongguoWebCategoryIdentityAndPagination(t *testing.T) {
	for _, category := range []string{"comic_series", "ai_series"} {
		route, name := hongguoWebGenre(category)
		body := hongguoWebCategoryFixture(route, 1, 2, "123", "123", "456")
		items, more, err := parseHongguoWebGenrePage(body, category, 1)
		if err != nil || !more || len(items) != 2 || items[0].CategoryName != name || items[0].ID != "hongguo:123" {
			t.Fatal("category parsing failed", category, items, more, err)
		}
		if _, _, err := parseHongguoWebGenrePage(body, category, 2); err == nil {
			t.Fatal("accepted mismatched page")
		}
		wrong := hongguoWebCategoryFixture("comic", 1, 1, "123")
		if _, _, err := parseHongguoWebGenrePage(wrong, category, 1); err == nil {
			t.Fatal("imported comic books as drama")
		}
		if _, _, err := parseHongguoWebGenrePage(hongguoWebCategoryFixture(route, 1, 2), category, 1); err == nil {
			t.Fatal("accepted empty initial page")
		}
		if _, more, err := parseHongguoWebGenrePage(hongguoWebCategoryFixture(route, 2, 2, "789"), category, 2); err != nil || more {
			t.Fatal("last page did not finish", err)
		}
	}
}

func TestHongguoWebCategoriesUseCanonicalRoutesAndPersist(t *testing.T) {
	fail := false
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if fail {
			return nil, errors.New("synthetic offline")
		}
		route := strings.TrimPrefix(request.URL.Path, "/category/")
		if request.Method != http.MethodGet || route != "comic-drama" && route != "ai-drama" || request.URL.Query().Get("tab") != "" {
			t.Fatal("wrong category route", request.URL)
		}
		page := 1
		if raw := request.URL.Query().Get("page"); raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		id := fmt.Sprintf("%d%d", map[string]int{"comic-drama": 1, "ai-drama": 2}[route], page)
		return sourceFixtureResponse(request, 200, hongguoWebCategoryFixture(route, page, 2, id)), nil
	})
	for _, category := range []string{"comic_series", "ai_series"} {
		for page := 1; page <= 2; page++ {
			result, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHongguo, Category: category, Page: page, Force: true})
			if err != nil || result.Warning != "" || len(result.Items) != 1 || result.Page != page || result.HasMore != (page == 1) {
				t.Fatal("category load failed", category, page, result, err)
			}
		}
	}
	fail = true
	if _, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHongguo, Category: "comic_series", Page: 3, Force: true}); err == nil {
		t.Fatal("offline request succeeded")
	}
	if len(engine.nativeCached("hongguo|comic_series").Items) != 2 {
		t.Fatal("offline request erased cached category")
	}
	restored := &nativeEngine{directory: engine.directory, catalogs: map[string][]nativeDrama{}, catalogStates: map[string]nativeCatalogState{}}
	restored.loadCatalogCache()
	for _, category := range []string{"comic_series", "ai_series"} {
		cached := restored.nativeCached(nativeCatalogKey(sourceHongguo, category))
		if len(cached.Items) != 2 || cached.Page != 2 || cached.HasMore || !cached.Fresh {
			t.Fatal("web cursor not restored", category, cached)
		}
	}
}

func TestHongguoLegacyCategoryCacheInvalidation(t *testing.T) {
	directory := t.TempDir()
	disk := nativeCatalogDisk{Version: 3, Catalogs: map[string][]nativeDrama{"hongguo|comic_series": {{ID: "hongguo:123", Source: sourceHongguo, Title: "保留旧剧"}}}, States: map[string]nativeCatalogState{
		"hongguo|comic_series": {Page: 40, HasMore: false, UpdatedAt: time.Now()},
		"hongguo|ai_series":    {Page: 30, HasMore: false, UpdatedAt: time.Now()},
	}}
	body, _ := json.Marshal(disk)
	if err := os.WriteFile(filepath.Join(directory, "catalogs.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	engine := &nativeEngine{directory: directory, catalogs: map[string][]nativeDrama{}, catalogStates: map[string]nativeCatalogState{}}
	engine.loadCatalogCache()
	for _, category := range []string{"comic_series", "ai_series"} {
		cached := engine.nativeCached(nativeCatalogKey(sourceHongguo, category))
		if cached.Page != 1 || !cached.HasMore || cached.Fresh {
			t.Fatal("old category state was reused", category, cached)
		}
	}
	if len(engine.nativeCached("hongguo|comic_series").Items) != 1 {
		t.Fatal("migration deleted user-visible cached dramas")
	}
}

func TestHongguoWebAggregateCursorRecovery(t *testing.T) {
	var pages []int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		page := 1
		if request.URL.Query().Get("page") != "" {
			page, _ = strconv.Atoi(request.URL.Query().Get("page"))
		}
		pages = append(pages, page)
		return sourceFixtureResponse(request, 200, hongguoWebCategoryFixture("comic-drama", page, 3, strconv.Itoa(page))), nil
	})
	if _, err := d.fetchHongguoAppCatalogCategory(context.Background(), "comic_series"); err != nil {
		t.Fatal(err)
	}
	state := d.hongguoCatalogSnapshot()
	restarted := sourceFixtureDownloader(t, d.client.Transport.(sourceFixtureTransport))
	restarted.restoreHongguoCatalog(state)
	ctx := context.WithValue(context.Background(), libraryMoreKey{}, true)
	if _, err := restarted.fetchHongguoAppCatalogCategory(ctx, "comic_series"); err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatal("aggregate web cursor restarted", pages)
	}
}

func TestHongguoAggregateIncludesBothWebGenres(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		route := strings.TrimPrefix(request.URL.Path, "/category/")
		id := map[string]string{"comic-drama": "123", "ai-drama": "456"}[route]
		if id == "" {
			t.Fatal("unexpected request", request.URL)
		}
		return sourceFixtureResponse(request, 200, hongguoWebCategoryFixture(route, 1, 1, id)), nil
	})
	d.hongguoClient().state.Feeds["short_play"] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	ctx := context.WithValue(context.Background(), libraryMoreKey{}, true)
	items, err := d.fetchHongguoAppCatalogCategory(ctx, "")
	if err != nil || len(items) != 2 || hongguoCatalogHasMore(d.hongguoCatalogSnapshot()) {
		t.Fatal("aggregate did not import both web categories", len(items), err)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.CategoryName]++
	}
	if counts["漫剧"] != 1 || counts["AI剧"] != 1 {
		t.Fatal("aggregate lost category identities", counts)
	}
}

func TestLiveHongguoWebCategoryTextOnly(t *testing.T) {
	if os.Getenv("CHECK_LIVE_HONGGUO_CATEGORIES") != "true" {
		t.Skip("set CHECK_LIVE_HONGGUO_CATEGORIES for category text-only diagnosis")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	for _, category := range []string{"comic_series", "ai_series"} {
		var first string
		for page := 1; page <= 2; page++ {
			items, more, err := engine.downloader.fetchHongguoWebGenrePage(context.Background(), category, page)
			if err != nil || len(items) == 0 {
				t.Fatal(category, page, err)
			}
			if page == 1 {
				first = items[0].ID
			} else if first == items[0].ID {
				t.Fatal("source returned the same first item for page 2")
			}
			t.Logf("category=%s page=%d count=%d more=%t first_id=%s; images/media not requested", category, page, len(items), more, items[0].ID)
		}
	}
}
