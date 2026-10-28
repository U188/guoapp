package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// xchinaPageSize 是站点分页的固定页大小。参考脚本 categoryContent 里
// limit 固定为 24，接口不返回 total，因此只能用「本页条数是否满页」推断还有下一页。
const xchinaPageSize = 24

// xchinaCategory 是站点的固定频道表。SPA 不提供分类接口，
// 频道即 /sevenVideos 的 type 参数，因此在这里静态声明。
type xchinaCategory struct {
	ID   string
	Name string
}

var xchinaCategories = []xchinaCategory{
	{ID: "recent", Name: "最新视频"},
	{ID: "hot", Name: "当前最热"},
	{ID: "week_hot", Name: "周榜"},
	{ID: "month_hot", Name: "月榜"},
	{ID: "high", Name: "站长推荐"},
	{ID: "5mins", Name: "5分钟+"},
	{ID: "10mins", Name: "10分钟+"},
	{ID: "c0", Name: "国产视频"},
	{ID: "c1", Name: "麻豆视频"},
	{ID: "c2", Name: "91制片厂"},
	{ID: "c3", Name: "玩偶姐姐"},
	{ID: "c4", Name: "小鸟酱专题"},
	{ID: "c5", Name: "糖心Vlog"},
	{ID: "c6", Name: "天美传媒"},
	{ID: "c7", Name: "蜜桃传媒"},
	{ID: "c8", Name: "皇家华人"},
	{ID: "c9", Name: "星空传媒"},
	{ID: "japan", Name: "日本AV"},
	{ID: "av", Name: "高清AV"},
	{ID: "uncensored", Name: "无码流出"},
	{ID: "west", Name: "欧美"},
}

// xchinaSortOptions 是分类页的排序筛选（对应 sortBy 参数）。
var xchinaSortOptions = []struct{ ID, Name string }{
	{ID: "latest", Name: "最新"},
	{ID: "views", Name: "最热"},
	{ID: "relevance", Name: "相关"},
	{ID: "duration", Name: "时长"},
}

func validXChinaCategory(category string) bool {
	category = strings.TrimSpace(category)
	if category == "" {
		return false
	}
	for _, entry := range xchinaCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func validXChinaSort(sort string) bool {
	sort = strings.TrimSpace(sort)
	if sort == "" {
		return true
	}
	for _, entry := range xchinaSortOptions {
		if entry.ID == sort {
			return true
		}
	}
	return false
}

func validXChinaID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return value != "." && value != ".."
}

func xchinaDurationSeconds(value any) int {
	text := strings.TrimSpace(nativeText(value))
	if text == "" {
		return 0
	}
	number, err := strconv.Atoi(text)
	if err != nil || number < 0 || number > 864000 {
		return 0
	}
	return number
}

// xchinaDramaFromMap 把 /sevenVideos 返回的单条记录转成仓库统一的 Drama。
func (d *Downloader) xchinaDramaFromMap(row map[string]any) (Drama, error) {
	id := strings.TrimSpace(mapString(row, "id"))
	if !validXChinaID(id) {
		return Drama{}, errors.New("站源返回的视频编号无效")
	}
	title := strings.TrimSpace(mapString(row, "title"))
	if title == "" {
		title = id
	}
	if vip, _ := row["vip"].(bool); vip {
		title = "[VIP] " + title
	}
	seconds := xchinaDurationSeconds(row["duration"])
	remark := strings.TrimSpace(mapString(row, "durationStr"))
	if remark == "" && seconds > 0 {
		remark = strconv.Itoa(seconds/60) + "分钟"
	}
	online := strings.TrimSpace(mapString(row, "time"))
	if len(online) >= 10 {
		online = online[:10]
	} else {
		online = ""
	}
	client := d.xchinaClient()
	cover := client.imageURL(id)
	description := truncate(strings.TrimSpace(mapString(row, "keywords_gem")), 12000)
	return Drama{
		ID: providerDramaID(sourceXChina, id), Source: sourceXChina, SourceID: id,
		Title: truncate(title, 256), Name: truncate(title, 256),
		Desc: description, Intro: description, Cover: cover, CoverURL: cover,
		Category: "视频", ChannelName: "XChina", CategoryName: strings.TrimSpace(mapString(row, "videoType")),
		Remark: truncate(remark, 64), OnlineDate: online,
		Tags: xchinaTags(row), Views: mapString(row, "views", "view_count"),
	}, nil
}

func xchinaTags(row map[string]any) []string {
	var tags []string
	seen := map[string]bool{}
	for _, key := range []string{"videoType", "user"} {
		value := strings.TrimSpace(mapString(row, key))
		if value != "" && len([]rune(value)) <= 48 && !seen[value] {
			seen[value] = true
			tags = append(tags, value)
		}
	}
	return tags
}

func xchinaRows(value any) []map[string]any {
	var rows []map[string]any
	// appendRows 直接向 rows 追加：站点可能返回裸数组，也可能包在容器对象里。
	appendRows := func(list []any) {
		for _, entry := range list {
			if row, ok := entry.(map[string]any); ok && len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}
	switch typed := value.(type) {
	case []any:
		appendRows(typed)
	case map[string]any:
		// 站点不同接口/版本的容器键不同，逐个探测而不是命中即停，
		// 避免某个键恰好在场但为空时把整页判成空列表。
		for _, key := range []string{"list", "items", "data", "videos", "rows", "result", "videoList"} {
			if list, ok := typed[key].([]any); ok {
				appendRows(list)
			}
		}
	}
	return rows
}

func (d *Downloader) fetchXChinaCategories(ctx context.Context) ([]nativeCategory, error) {
	var categories []nativeCategory
	for _, entry := range xchinaCategories {
		categories = append(categories, nativeCategory{ID: entry.ID, Name: entry.Name})
	}
	return categories, nil
}

func (d *Downloader) fetchXChinaCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 1000000 || len(query) > 1024 {
		return nil, false, errors.New("站源目录查询参数无效")
	}
	client := d.xchinaClient()
	var payload any
	var err error
	switch {
	case query != "":
		payload, err = client.call(ctx, "/searchSevenVideos", map[string]any{
			"page": page, "keywords": query, "filterCategory": "all",
			"filterTimeInDays": "all", "filterViews": "all",
			"filterDurationInMins": "all", "sortBy": "relevance",
		})
	default:
		typeID := strings.TrimSpace(category)
		if typeID == "" {
			typeID = "recent"
		}
		if !validXChinaCategory(typeID) {
			return nil, false, errors.New("站源分类无效")
		}
		payload, err = client.call(ctx, "/sevenVideos?page="+strconv.Itoa(page)+"&type="+url.QueryEscape(typeID), nil)
	}
	if err != nil {
		return nil, false, err
	}
	rows := xchinaRows(payload)
	if len(rows) > 500 {
		return nil, false, errors.New("站源目录返回条目过多")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		drama, err := d.xchinaDramaFromMap(row)
		if err != nil || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items, len(rows) >= xchinaPageSize, nil
}

// fetchXChinaCatalogPageSorted 处理分类页的排序筛选：sortBy 非空时走搜索接口，
// 接口无结果再回落到普通频道，避免排序参数把列表打成空。
func (d *Downloader) fetchXChinaCatalogPageSorted(ctx context.Context, page int, category, sort string) ([]Drama, bool, error) {
	if page < 1 || page > 1000000 || !validXChinaCategory(category) || !validXChinaSort(sort) {
		return nil, false, errors.New("站源目录查询参数无效")
	}
	if sort == "" {
		return d.fetchXChinaCatalogPage(ctx, page, category, "")
	}
	client := d.xchinaClient()
	payload, err := client.call(ctx, "/searchSevenVideos", map[string]any{
		"page": page, "keywords": "", "filterCategory": category,
		"filterTimeInDays": "all", "filterViews": "all",
		"filterDurationInMins": "all", "sortBy": sort,
	})
	if err != nil {
		return nil, false, err
	}
	rows := xchinaRows(payload)
	if len(rows) == 0 {
		return d.fetchXChinaCatalogPage(ctx, page, category, "")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		drama, err := d.xchinaDramaFromMap(row)
		if err != nil || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items, len(rows) >= xchinaPageSize, nil
}

func (d *Downloader) fetchXChinaDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !validXChinaID(sourceID) {
		return Drama{}, nil, errors.New("站源视频编号无效")
	}
	client := d.xchinaClient()
	payload, err := client.call(ctx, "/sevenVideos/"+url.PathEscape(sourceID), map[string]any{"url_search": ""})
	row, ok := payload.(map[string]any)
	// 详情请求失败时回退到最近一次成功响应，保证详情页可读。
	// 注意：缓存里的 m3u8 地址带秒级签名、必然已过期，只可用于展示，
	// 取流必须走 xchinaPlaylistURL 重新请求详情换新签名，不得复用这里的缓存。
	if err != nil || !ok || strings.TrimSpace(mapString(row, "id")) == "" {
		if cached := client.recall(sourceID); cached != nil {
			row, ok = cached, true
		} else if err != nil {
			return Drama{}, nil, err
		} else {
			return Drama{}, nil, errors.New("站源未返回该视频详情")
		}
	}
	client.remember(sourceID, row)
	drama, err := d.xchinaDramaFromMap(row)
	if err != nil {
		return Drama{}, nil, err
	}
	chapter := Chapter{
		ID: providerChapterID(sourceXChina, sourceID, "main"), Source: sourceXChina,
		Title: "正片", CurrentEpisode: rawEpisode(1), VideoURL: "xchina-v|" + sourceID,
		PageURL: client.siteURL() + "/video/" + url.PathEscape(sourceID), Referer: client.siteURL() + "/",
	}
	return drama, []Chapter{chapter}, nil
}

// xchinaPlaylist 取出当前有效的 m3u8 地址。站点签名是秒级的、无容差，
// 因此每次播放都必须重新请求详情换新签名，绝不能复用旧地址。
func (d *Downloader) xchinaPlaylistURL(ctx context.Context, sourceID string) (string, error) {
	client := d.xchinaClient()
	payload, err := client.call(ctx, "/sevenVideos/"+url.PathEscape(sourceID), map[string]any{"url_search": ""})
	if err != nil {
		return "", err
	}
	row, ok := payload.(map[string]any)
	if !ok {
		return "", errors.New("站源未返回视频详情")
	}
	client.remember(sourceID, row)
	address := xchinaFirstPlaylist(row)
	if address == "" {
		return "", errors.New("站源未返回播放地址，可能需要刷新详情后重试")
	}
	site := client.siteURL()
	if !isProviderHTTPMediaURL(address) {
		address = resolveProviderURL(site+"/", address)
	}
	if !isProviderHTTPMediaURL(address) {
		return "", errors.New("站源播放地址无效")
	}
	return address, nil
}

func xchinaFirstPlaylist(row map[string]any) string {
	if list, ok := row["m3u8s"].([]any); ok {
		for _, entry := range list {
			if text := strings.TrimSpace(nativeText(entry)); text != "" {
				return text
			}
		}
	}
	if text := strings.TrimSpace(nativeText(row["m3u8"])); text != "" {
		return text
	}
	return ""
}

func (d *Downloader) resolveXChinaMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceXChina, sourceID, "")
	if !valid || source != sourceXChina || !validXChinaID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("站源播放分集信息无效，请刷新详情")
	}
	address, err := d.xchinaPlaylistURL(ctx, sourceID)
	if err != nil {
		return providerMedia{}, err
	}
	site := d.xchinaClient().siteURL()
	media := providerMedia{URL: address, Referer: site + "/"}
	media, err = d.prepareWebProviderMedia(ctx, media, "XChina")
	if err != nil {
		return providerMedia{}, fmt.Errorf("XChina 取流失败：%w", err)
	}
	return media, nil
}
