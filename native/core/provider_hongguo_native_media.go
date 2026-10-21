package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("红果视频 ID 无效")
	}
	payload := map[string]any{
		"video_id": videoID, "content_type": 1,
		"biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_model/v1/", nil, payload)
	if err != nil {
		return providerMedia{}, err
	}
	data := nestedMap(result, "data")
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok {
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil {
			return providerMedia{}, errors.New("红果 App 播放信息格式异常")
		}
	}
	return selectHongguoAppMedia(model)
}

func selectHongguoAppMedia(model map[string]any) (providerMedia, error) {
	variants := hongguoVideoVariants(model)
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)
	var keyErr error
	type scoredMedia struct {
		media providerMedia
		score int
	}
	var choices []scoredMedia
	for _, entry := range variants {
		variant, qualityHint := entry.row, entry.quality
		meta := nestedMap(variant, "video_meta")
		codec := strings.ToLower(mapString(meta, "codec_type"))
		if codec == "bytevc2" || strings.Contains(strings.ToLower(mapString(variant, "gear_des_key")), "bytevc2") {
			continue
		}
		addresses := hongguoMediaAddresses(variant)
		if len(addresses) == 0 {
			continue
		}
		media := providerMedia{Referer: "https://novel.snssdk.com/", Duration: time.Duration(duration * float64(time.Second))}
		encryption := nestedMap(variant, "encrypt_info")
		spade := mapString(encryption, "spade_a")
		if spade != "" || encryption["encrypt"] == true || mapString(encryption, "encryption_method") == "cenc-aes-ctr" {
			var err error
			media.CENCKey, err = hongguoContentKey(spade)
			if err != nil {
				keyErr = err
				continue
			}
		}
		height, _ := strconv.Atoi(mapString(meta, "vheight"))
		if definition, err := strconv.Atoi(hongguoQualityNumber.FindString(mapString(meta, "definition"))); err == nil && definition > 0 {
			height = definition
		} else if width, _ := strconv.Atoi(mapString(meta, "vwidth")); width > 0 && (height == 0 || width < height) {
			height = width
		}
		if height <= 0 {
			height = qualityHint
		}
		media.Quality = height
		quality := height * 10
		if codec == "h264" || codec == "avc1" {
			quality++
		}
		for _, address := range addresses {
			media.URL = address
			choices = append(choices, scoredMedia{media: media, score: quality})
		}
	}
	if len(choices) > 0 {
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
		selected := choices[0].media
		for _, choice := range choices {
			selected.Variants = append(selected.Variants, choice.media)
		}
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, fmt.Errorf("红果 App 媒体密钥不可用: %w", keyErr)
	}
	return providerMedia{}, errors.New("红果 App 未返回兼容的媒体，已跳过不支持的编码")
}

func hongguoMediaAddresses(info map[string]any) []string {
	var addresses []string
	seen := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case string:
			address := strings.TrimSpace(value)
			if len(address) > 8192 {
				return
			}
			if !isProviderHTTPMediaURL(address) {
				decoded, err := decodeHongguoBase64(address)
				if err != nil {
					return
				}
				address = strings.TrimSpace(string(decoded))
			}
			if isProviderHTTPMediaURL(address) && !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		case []any:
			for _, item := range value {
				add(item)
			}
		}
	}
	for _, key := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list", "play_addr", "url"} {
		add(info[key])
	}
	return addresses
}

type hongguoVariant struct {
	row     map[string]any
	quality int
}

func hongguoVideoVariants(model map[string]any) []hongguoVariant {
	if rows := hongguoVariantRows(model["video_list"], 0, 0); len(rows) > 0 {
		return rows
	}
	for _, key := range []string{"dynamic_video_list", "dynamic_video", "video_info", "data"} {
		if rows := hongguoVariantRows(model[key], 0, 0); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func hongguoVariantRows(value any, hint, depth int) []hongguoVariant {
	if depth > 4 {
		return nil
	}
	switch value := value.(type) {
	case []any:
		var rows []hongguoVariant
		for _, item := range value {
			rows = append(rows, hongguoVariantRows(item, hint, depth+1)...)
			if len(rows) >= 200 {
				break
			}
		}
		return rows
	case map[string]any:
		if hongguoVariantRow(value) {
			return []hongguoVariant{{row: value, quality: hint}}
		}
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var rows []hongguoVariant
		for _, key := range keys {
			switch value[key].(type) {
			case map[string]any, []any:
				child := hint
				if number, err := strconv.Atoi(hongguoQualityNumber.FindString(key)); err == nil && number > 0 {
					child = number
				}
				rows = append(rows, hongguoVariantRows(value[key], child, depth+1)...)
				if len(rows) >= 200 {
					return rows
				}
			}
		}
		return rows
	}
	return nil
}

func hongguoVariantRow(value map[string]any) bool {
	for _, key := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list", "play_addr", "spade_a", "encrypt_info"} {
		if value[key] != nil {
			return true
		}
	}
	return false
}
