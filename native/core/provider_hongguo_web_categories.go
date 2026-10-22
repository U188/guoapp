package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func hongguoWebGenre(category string) (string, string) {
	switch category {
	case "comic_series":
		return "comic-drama", "漫剧"
	case "ai_series":
		return "ai-drama", "AI剧"
	}
	return "", ""
}

func (d *Downloader) fetchHongguoWebGenrePage(ctx context.Context, category string, page int) ([]Drama, bool, error) {
	route, name := hongguoWebGenre(category)
	if route == "" || page < 1 || page > 1000000 {
		return nil, false, errors.New("红果网页分类或页码无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, providerTextUserAgentKey{}, "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	ctx = context.WithValue(ctx, providerTextNoCacheKey{}, true)
	address := d.providerBaseURL(sourceHongguo) + "/category/" + route
	if page > 1 {
		address += "?page=" + strconv.Itoa(page)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
		body, err := d.fetchProviderText(ctx, address, d.providerBaseURL(sourceHongguo)+"/")
		if err != nil {
			d.recordDiagnostic(diagnosticEvent{Level: "error", Event: "hongguo_category_web", Source: sourceHongguo, Message: fmt.Sprintf("category=%s page=%d request_failed=%v", category, page, publicError(err))})
			return nil, false, err
		}
		items, more, err := parseHongguoWebGenrePage(body, category, page)
		if err == nil {
			d.recordDiagnostic(diagnosticEvent{Level: "info", Event: "hongguo_category_web", Source: sourceHongguo, TotalBytes: int64(len(body)), Message: fmt.Sprintf("category=%s name=%s route=%s page=%d items=%d more=%t", category, name, route, page, len(items), more)})
			return items, more, nil
		}
		lastErr = err
	}
	d.recordDiagnostic(diagnosticEvent{Level: "error", Event: "hongguo_category_web", Source: sourceHongguo, Message: fmt.Sprintf("category=%s page=%d parse_failed=%v", category, page, lastErr)})
	return nil, false, lastErr
}

func parseHongguoWebGenrePage(body, category string, requestedPage int) ([]Drama, bool, error) {
	route, name := hongguoWebGenre(category)
	page := routerLoaderMap(parseRouterData(body), "category_$", "category_page")
	if route == "" || len(page) == 0 || page["isSuccess"] == false {
		return nil, false, errors.New("红果网页分类数据不可用")
	}
	identity := mapString(nestedMap(page, "categoryRoute"), "contentType")
	if identity != route {
		return nil, false, errors.New("红果网页返回了其他内容分类，已停止导入")
	}
	if canonical := mapString(page, "canonicalUrl"); canonical != "" {
		address, err := url.Parse(canonical)
		if err != nil || strings.TrimRight(address.Path, "/") != "/category/"+route {
			return nil, false, errors.New("红果分类规范地址不匹配")
		}
	}
	pagination := nestedMap(page, "pagination")
	actual, valid := webProviderInteger(pagination["pageNum"], 1000000)
	totalPages, totalValid := webProviderInteger(pagination["totalPages"], 1000000)
	if !valid || actual != requestedPage || !totalValid || totalPages < actual {
		return nil, false, errors.New("红果网页分类分页信息不匹配，已保留上次位置")
	}
	rows, valid := page["recommendList"].([]any)
	if !valid || len(rows) > 500 {
		return nil, false, errors.New("红果网页分类条目格式无效")
	}
	var items []Drama
	seen := map[string]bool{}
	for _, row := range rows {
		drama := hongguoDramaFromAny(row, name)
		if drama.ID == "" || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	if len(items) == 0 && (requestedPage == 1 || len(rows) > 0 || requestedPage < totalPages) {
		return nil, false, errors.New("红果网页分类未返回应有的剧集，请重试")
	}
	return items, requestedPage < totalPages, nil
}

func (d *Downloader) scanHongguoWebGenre(ctx context.Context, category string) ([]Drama, error) {
	client := d.hongguoClient()
	key := "web:" + category
	more, _ := ctx.Value(libraryMoreKey{}).(bool)
	cursor := d.hongguoCatalogSnapshot().Feeds[key]
	if more && cursor.Exhausted {
		return nil, nil
	}
	page := 1
	if more && cursor.Initialized {
		page = cursor.Offset + 1
	}
	rows, hasMore, err := d.fetchHongguoWebGenrePage(ctx, category, page)
	if err != nil {
		return nil, err
	}
	next := hongguoCatalogCursor{Offset: page, Initialized: true, Exhausted: !hasMore, UpdatedAt: time.Now()}
	if !more && cursor.Initialized && cursor.Offset > page {
		next = cursor
	}
	client.mu.Lock()
	client.state.Feeds[key] = next
	client.mu.Unlock()
	reportLibraryProgress(ctx, sourceHongguo, rows, nil, false)
	return rows, nil
}

func hongguoWebCategoryCache(key string) bool {
	return key == nativeCatalogKey(sourceHongguo, "comic_series") || key == nativeCatalogKey(sourceHongguo, "ai_series")
}
