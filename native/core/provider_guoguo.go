package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	guoguoAPIHost       = "5xe5vxqg.e8rgzshf.cc"
	guoguoAESKey        = "GcgzsKdDZTumABNz7uujrCfPIk9TQ355"
	guoguoSID           = "240b270a41a94188b7d957f7fbb58f48"
	guoguoDeviceID      = "ac7e0c9f0a97d3158444573c40def879"
	guoguoFingerprint   = "281e9aa2daceabf1e854336163bd7b1671a065b9d896f26a4011b957fa1c61e3"
	guoguoDeviceModel   = "2407FRK8EC"
	guoguoDeviceBrand   = "Redmi"
	guoguoSystemVersion = "16"
	guoguoSDKVersion    = "1.4.7"
	guoguoAppVersion    = "1.1.0"
	guoguoFPVersion     = "3.0.0"
	guoguoMacct         = "sf117"
	guoguoUserAgent     = "Dart/3.4 (dart:io)"
	guoguoMediaMark     = "guoguo-media://"
	guoguoLoginBody     = "frH4CSJ21fbRgdTUM+jDVLwWNn6QrSGkLLSCnPJ8i12EypJdBIJJUswCFacOTLNzzTsVl32bqAotMmj5v6Ot8H96x+mfdpOQ5icCi7FhpUIEAxdK0CA6BGxkEfvpz7DXBP1DbYuf/XB9whG1BSHFOixcTUhK2EgTe1PaE4HLHvY="
)

var (
	guoguoAPIHosts = []string{guoguoAPIHost, "18.167.253.162"}
	guoguoCDNHosts = []string{"mysf5bf09.hzlld56.com", "mysf5bf10.zheguo00.com", "ycsf4bf05.ablog168.com", "ycsf4bf06.klxq888.com"}

	guoguoSkipFirst = []string{"次元", "直播", "我的", "AI科技", "导航站", "广场"}
	guoguoSkipNames = []string{"最新", "视频·最新", "AI科技"}
)

type guoguoClient struct {
	mu        sync.Mutex
	host      string
	token     string
	loggedAt  time.Time
	mediaType map[string]int
}

func (d *Downloader) guoguo() *guoguoClient {
	d.guoguoOnce.Do(func() {
		d.guoguoCore = &guoguoClient{host: guoguoAPIHost, mediaType: map[string]int{}}
	})
	return d.guoguoCore
}

func guoguoHeaders(token string) map[string]string {
	headers := map[string]string{
		"sdk_version":        guoguoSDKVersion,
		"user-agent":         guoguoUserAgent,
		"sid":                guoguoSID,
		"app_version":        guoguoAppVersion,
		"accept-encoding":    "gzip",
		"os":                 "1",
		"device_fingerprint": guoguoFingerprint,
		"content-type":       "application/json; charset=UTF-8",
		"device_model":       guoguoDeviceModel,
		"fp_version":         guoguoFPVersion,
		"accept":             "application/json,*/*",
		"system_name":        "Android",
		"user_agent":         "",
		"ver":                guoguoAppVersion,
		"device":             "Android",
		"device_brand":       guoguoDeviceBrand,
		"device_id":          guoguoDeviceID,
		"system_version":     guoguoSystemVersion,
		"macct":              guoguoMacct,
		"client_ts":          strconv.FormatInt(time.Now().Unix(), 10),
	}
	if token != "" {
		headers["token"] = token
	}
	return headers
}

func guoguoDecrypt(value string) ([]byte, error) {
	text := strings.Trim(strings.TrimSpace(value), `"`)
	if text == "" {
		return nil, errors.New("果果响应为空")
	}
	if padding := len(text) % 4; padding != 0 {
		text += strings.Repeat("=", 4-padding)
	}
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(text, "="))
	}
	if err != nil || len(raw) < 16 {
		return nil, errors.New("果果响应编码无效")
	}
	block, err := aes.NewCipher([]byte(guoguoAESKey))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(raw)/16*16)
	for offset := 0; offset < len(plain); offset += 16 {
		block.Decrypt(plain[offset:offset+16], raw[offset:offset+16])
	}
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		plain = bytes.TrimRight(plain, "\x00")
	}
	return plain, nil
}

func (c *guoguoClient) raw(ctx context.Context, d *Downloader, host, path string, body map[string]any, token string) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	for key, value := range guoguoHeaders(token) {
		request.Header.Set(key, value)
	}
	response, err := d.doPreparedCatalogRequest(request, 20*time.Second, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("果果接口 HTTP %d", response.StatusCode)
	}
	return content, nil
}

func guoguoParse(content []byte) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(content))
	var direct map[string]any
	if err := json.Unmarshal([]byte(trimmed), &direct); err == nil {
		if len(direct) > 0 {
			return direct, nil
		}
	}
	var wrapped string
	if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil && wrapped != "" {
		trimmed = wrapped
	}
	plain, err := guoguoDecrypt(trimmed)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(plain, &decoded); err != nil {
		return nil, errors.New("果果响应解析失败")
	}
	return decoded, nil
}

func (c *guoguoClient) login(ctx context.Context, d *Downloader) error {
	c.mu.Lock()
	if c.token != "" && time.Since(c.loggedAt) < 10*time.Minute {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	for _, host := range guoguoAPIHosts {
		content, err := c.raw(ctx, d, host, "/front/cluser/c/user/mac/login", map[string]any{"encrypt": guoguoLoginBody}, "")
		if err != nil {
			continue
		}
		parsed, err := guoguoParse(content)
		if err != nil {
			continue
		}
		token := mapString(nestedMap(parsed, "data"), "token")
		if mapString(parsed, "code") == "0" && token != "" {
			c.mu.Lock()
			c.token, c.host, c.loggedAt = token, host, time.Now()
			c.mu.Unlock()
			return nil
		}
	}
	return errors.New("果果登录失败")
}

func (c *guoguoClient) call(ctx context.Context, d *Downloader, path string, body map[string]any) (map[string]any, error) {
	if err := c.login(ctx, d); err != nil {
		return nil, err
	}
	c.mu.Lock()
	hosts := append([]string{c.host}, guoguoAPIHosts...)
	token := c.token
	c.mu.Unlock()
	var lastErr error
	for _, host := range hosts {
		content, err := c.raw(ctx, d, host, path, body, token)
		if err != nil {
			lastErr = err
			continue
		}
		parsed, err := guoguoParse(content)
		if err != nil {
			lastErr = err
			continue
		}
		switch mapString(parsed, "code") {
		case "0":
			c.mu.Lock()
			c.host = host
			c.mu.Unlock()
			return parsed, nil
		case "6":
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			if err := c.login(ctx, d); err != nil {
				return nil, err
			}
			continue
		default:
			lastErr = fmt.Errorf("果果接口返回 %s", truncate(mapString(parsed, "msg"), 40))
		}
	}
	if lastErr == nil {
		lastErr = errors.New("果果接口暂时不可用")
	}
	return nil, lastErr
}

func (c *guoguoClient) mediaTypeFor(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value := c.mediaType[id]; value > 0 {
		return value
	}
	return 1
}

func (c *guoguoClient) rememberMediaType(id string, value int) {
	c.mu.Lock()
	if len(c.mediaType) >= 2000 {
		c.mediaType = map[string]int{}
	}
	c.mediaType[id] = value
	c.mu.Unlock()
}

func guoguoPicture(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	if strings.HasSuffix(strings.ToLower(path), ".bnc") {
		path = path[:len(path)-4]
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "https://" + guoguoCDNHosts[0] + path
}

func guoguoPlayURL(video map[string]any) string {
	address := strings.TrimSpace(mapString(video, "videoUrl"))
	if address == "" {
		return ""
	}
	if strings.HasPrefix(address, "http") {
		return address
	}
	if !strings.HasPrefix(address, "/") {
		address = "/" + address
	}
	return "https://" + guoguoCDNHosts[0] + address
}

func guoguoRows(value any) []map[string]any {
	data, _ := value.(map[string]any)
	if data == nil {
		return nil
	}
	var rows []map[string]any
	collect := func(entries any) {
		for _, entry := range anyList(entries) {
			if row, ok := entry.(map[string]any); ok && (mapString(row, "mediaId") != "" || row["video"] != nil) {
				rows = append(rows, row)
			}
		}
	}
	for _, group := range anyList(data["dataList"]) {
		groupMap, _ := group.(map[string]any)
		if groupMap == nil {
			continue
		}
		if list := anyList(groupMap["mediaList"]); len(list) > 0 {
			collect(list)
			continue
		}
		if list := anyList(groupMap["items"]); len(list) > 0 {
			collect(list)
			continue
		}
		if mapString(groupMap, "mediaId") != "" || groupMap["video"] != nil {
			rows = append(rows, groupMap)
		}
	}
	collect(data["list"])
	collect(data["records"])
	return rows
}

func (d *Downloader) guoguoDrama(row map[string]any) Drama {
	video, _ := row["video"].(map[string]any)
	mediaID := strings.TrimSpace(mapString(row, "mediaId"))
	if mediaID == "" {
		return Drama{}
	}
	d.guoguo().rememberMediaType(mediaID, guoguoInt(mapString(row, "mediaType"), 1))
	title := strings.TrimSpace(mapString(video, "videoTitle"))
	if title == "" {
		title = "作品" + mediaID
	}
	remark := ""
	if duration := guoguoInt(mapString(video, "videoDuration"), 0); duration > 0 {
		remark = strconv.Itoa((duration+59)/60) + "分钟"
	}
	locked := false
	switch mapString(row, "payType") {
	case "2", "4":
		locked = true
		remark = strings.TrimSpace(remark + "·需解锁")
	}
	return Drama{
		ID:          providerDramaID(sourceGuoguo, mediaID),
		Source:      sourceGuoguo,
		SourceID:    mediaID,
		Title:       title,
		Name:        title,
		Cover:       guoguoPicture(mapString(video, "videoCoverImg")),
		Remark:      remark,
		ChannelName: "果果",
		VIP:         guoguoBoolPointer(locked),
	}
}

func guoguoBoolPointer(value bool) *bool {
	return &value
}

func guoguoInt(value string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return fallback
}

func (d *Downloader) fetchGuoguoCategories(ctx context.Context) ([]nativeCategory, error) {
	client := d.guoguo()
	result, err := client.call(ctx, d, "/front/media/category/listAllCategory", map[string]any{})
	if err != nil {
		return nil, err
	}
	var categories []nativeCategory
	for _, entry := range anyList(result["data"]) {
		first, _ := entry.(map[string]any)
		if first == nil {
			continue
		}
		firstName := strings.TrimSpace(mapString(first, "firstCategoryName"))
		if firstName == "" {
			firstName = "分类"
		}
		if slicesContain(guoguoSkipFirst, firstName) {
			continue
		}
		children := anyList(first["child"])
		added := 0
		for _, child := range children {
			second, _ := child.(map[string]any)
			if second == nil {
				continue
			}
			secondName := strings.TrimSpace(mapString(second, "secondCategoryName"))
			secondID := strings.TrimSpace(mapString(second, "categoryId"))
			if secondID == "" || slicesContain(guoguoSkipNames, secondName) {
				continue
			}
			categories = append(categories, nativeCategory{ID: firstName + ":" + secondID, Name: firstName + " · " + secondName})
			added++
		}
		if added == 0 {
			if firstID := strings.TrimSpace(mapString(first, "categoryId")); firstID != "" {
				categories = append(categories, nativeCategory{ID: firstName + ":" + firstID, Name: firstName})
			}
		}
	}
	return categories, nil
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validGuoguoCategory(category string) bool {
	parts := strings.SplitN(strings.TrimSpace(category), ":", 2)
	return len(parts) == 2 && strings.TrimSpace(parts[1]) != ""
}

func (d *Downloader) fetchGuoguoCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	client := d.guoguo()
	var result map[string]any
	var err error
	if strings.TrimSpace(query) != "" {
		result, err = client.call(ctx, d, "/front/media/listMediaBySearchType", map[string]any{
			"mediaType": 1, "searchKeyword": strings.TrimSpace(query), "pageNo": page, "pageSize": 24, "orderType": 1,
		})
	} else {
		categoryID := "1959159271920439296"
		if validGuoguoCategory(category) {
			categoryID = strings.TrimSpace(strings.SplitN(category, ":", 2)[1])
		}
		result, err = client.call(ctx, d, "/front/media/listAllBySecondCategoryIdNew", map[string]any{
			"categoryId": categoryID, "pageNo": page, "pageSize": 24,
		})
	}
	if err != nil {
		return nil, false, err
	}
	rows := guoguoRows(nestedMap(result, "data"))
	items := make([]Drama, 0, len(rows))
	for _, row := range rows {
		if drama := d.guoguoDrama(row); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, len(items) >= 24, nil
}

func (d *Downloader) fetchGuoguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || strings.ContainsAny(sourceID, "/?#") {
		return Drama{}, nil, errors.New("果果剧集 ID 无效")
	}
	client := d.guoguo()
	mediaType := client.mediaTypeFor(sourceID)
	result, err := client.call(ctx, d, "/front/media/getMediaById", map[string]any{"mediaId": sourceID, "mediaType": mediaType})
	if err != nil {
		return Drama{}, nil, err
	}
	if mapString(result, "code") == "2" && mediaType != 2 {
		client.rememberMediaType(sourceID, 2)
		result, err = client.call(ctx, d, "/front/media/getMediaById", map[string]any{"mediaId": sourceID, "mediaType": 2})
		if err != nil {
			return Drama{}, nil, err
		}
	}
	detail := nestedMap(result, "data")
	video, _ := detail["video"].(map[string]any)
	if len(video) == 0 {
		return Drama{}, nil, errors.New("果果未返回该视频资料")
	}
	title := strings.TrimSpace(mapString(video, "videoTitle"))
	if title == "" {
		title = "作品" + sourceID
	}
	var tags []string
	for _, entry := range anyList(detail["tagList"]) {
		if tag, ok := entry.(map[string]any); ok {
			if name := strings.TrimSpace(mapString(tag, "tagName")); name != "" {
				tags = append(tags, name)
			}
		}
	}
	locked := false
	switch mapString(detail, "payType") {
	case "2", "4":
		locked = true
	}
	drama := Drama{
		ID:          providerDramaID(sourceGuoguo, sourceID),
		Source:      sourceGuoguo,
		SourceID:    sourceID,
		Title:       title,
		Name:        title,
		Cover:       guoguoPicture(mapString(video, "videoCoverImg")),
		Intro:       mapString(video, "videoDescription"),
		Tags:        tags,
		ChannelName: "果果",
		VIP:         guoguoBoolPointer(locked),
	}
	chapters := []Chapter{{
		ID:             providerChapterID(sourceGuoguo, sourceID, "main"),
		Source:         sourceGuoguo,
		Title:          "正片",
		VideoURL:       guoguoMediaMark + sourceID,
		CurrentEpisode: rawEpisode(1),
		VIP:            locked,
	}}
	return drama, chapters, nil
}

func (d *Downloader) resolveGuoguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	mediaID := strings.TrimSpace(strings.TrimPrefix(task.Chapter.VideoURL, guoguoMediaMark))
	if mediaID == "" {
		if _, sourceID, ok := splitProviderDramaID(task.DramaID); ok {
			mediaID = sourceID
		}
	}
	if mediaID == "" || strings.ContainsAny(mediaID, "/?#") {
		return providerMedia{}, errors.New("果果播放请求缺少有效的视频 ID")
	}
	client := d.guoguo()
	result, err := client.call(ctx, d, "/front/media/getMediaById", map[string]any{"mediaId": mediaID, "mediaType": client.mediaTypeFor(mediaID)})
	if err != nil {
		return providerMedia{}, err
	}
	video, _ := nestedMap(result, "data")["video"].(map[string]any)
	address := guoguoPlayURL(video)
	if address == "" {
		return providerMedia{}, errors.New("果果该视频未返回播放地址")
	}
	if strings.Contains(address, "_preview") {
		return providerMedia{}, errors.New("果果该集需要解锁后才能播放，应用不会绕过站方付费限制")
	}
	return providerMedia{
		URL:     address,
		Referer: "https://" + guoguoCDNHosts[0] + "/",
		credentials: &providerMediaCredentials{
			userAgent: guoguoUserAgent,
		},
	}, nil
}
