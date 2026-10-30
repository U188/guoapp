package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const hongguoMultiVideoPath = "/novel/player/multi_video_model/v1/"

// 红果取流签名与设备绑定：随机 did/iid 会在 x-argus 校验后被服务端拒绝（HTTP 200 空体），
// 必须使用签名库内置的已注册设备（与参考实现一致）。
const (
	hongguoVideoDeviceID  = "4263275890506393"
	hongguoVideoInstallID = "2715163518305450"
)

type hongguoQueryField struct{ name, value string }

// hongguoVideoIdentity 描述取流请求使用的 App 身份。
// 站点已切换到新的 App 版本（aid=1967 / novelapp / 7.2.4.32），旧身份（8662 / novelread）
// 仍能取目录但取流会返回空体；保留旧身份只为对齐历史签名向量。
type hongguoVideoIdentity struct {
	Aid, AppName, VersionCode, VersionName, Channel, Resolution string
}

var (
	hongguoVideoIdentityLegacy = hongguoVideoIdentity{
		Aid: "8662", AppName: "novelread", VersionCode: "71332",
		VersionName: "7.1.3.32", Channel: "update_64", Resolution: "1280*2772",
	}
	hongguoVideoIdentityCurrent = hongguoVideoIdentity{
		Aid: "1967", AppName: "novelapp", VersionCode: "72432",
		VersionName: "7.2.4.32", Channel: "xiaomi_1967_64", Resolution: "1280*2620",
	}
)

func hongguoVideoQuery(deviceID, installID string, now time.Time, identity hongguoVideoIdentity) string {
	fields := []hongguoQueryField{
		{"iid", installID}, {"device_id", deviceID}, {"ac", "wifi"}, {"channel", identity.Channel},
		{"aid", identity.Aid}, {"app_name", identity.AppName}, {"version_code", identity.VersionCode}, {"version_name", identity.VersionName},
		{"device_platform", "android"}, {"os", "android"}, {"ssmix", "a"}, {"device_type", "25053RT47C"},
		{"device_brand", "Redmi"}, {"language", "zh"}, {"os_api", "36"}, {"os_version", "16"},
		{"manifest_version_code", identity.VersionCode}, {"resolution", identity.Resolution}, {"dpi", "520"}, {"update_version_code", identity.VersionCode},
		{"host_abi", "arm64-v8a"}, {"dragon_device_type", "phone"}, {"pv_player", identity.VersionCode},
		{"compliance_status", "0"}, {"need_personal_recommend", "1"}, {"player_so_load", "1"}, {"is_android_pad_screen", "0"},
		{"ts", strconv.FormatInt(now.Unix(), 10)}, {"_rticket", strconv.FormatInt(now.UnixMilli(), 10)},
	}
	var result []string
	for _, field := range fields {
		encode := func(v string) string {
			return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(v), "+", "%20"), "%2A", "*")
		}
		result = append(result, encode(field.name)+"="+encode(field.value))
	}
	return strings.Join(result, "&")
}

func hongguoVideoPayload(videoID string) ([]byte, error) {
	payload := struct {
		Biz struct {
			DetailPageVersion int  `json:"detail_page_version"`
			DeviceLevel       int  `json:"device_level"`
			DisableDiggStat   bool `json:"disable_digg_stat"`
			NeedAllDefinition bool `json:"need_all_video_definition"`
			NeedMP4Align      bool `json:"need_mp4_align"`
			UseOSPlayer       bool `json:"use_os_player"`
			UseServerDNS      bool `json:"use_server_dns"`
			VideoPlatform     int  `json:"video_platform"`
		} `json:"biz_param"`
		Mixed map[string][]string `json:"mixed_video_id_map"`
	}{}
	payload.Biz.DeviceLevel = 3
	payload.Biz.NeedAllDefinition = true
	payload.Biz.VideoPlatform = 1024
	payload.Mixed = map[string][]string{"1004": {videoID}}
	return json.Marshal(payload)
}

func (d *Downloader) hongguoVideoModelRequest(ctx context.Context, videoID string) (map[string]any, error) {
	body, err := hongguoVideoPayload(videoID)
	if err != nil {
		return nil, err
	}
	client := d.hongguoClient()
	client.mu.Lock()
	base := client.baseURL
	deviceID, installID := hongguoVideoDeviceID, hongguoVideoInstallID
	client.mu.Unlock()
	attempts := max(1, min(d.cfg.Retries, 3))
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+hongguoMultiVideoPath, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", hongguoVideoAppUserAgent)
		request.Header.Set("Accept", "application/json; charset=utf-8,application/x-protobuf")
		request.Header.Set("Content-Type", "application/json; charset=UTF-8")
		request.Header.Set("X-XS-From-Web", "0")
		request.Header.Set("X-TT-Request-Tag", "t=0;n=0")
		request.Header.Set("Sdk-Version", "2")
		request.Header.Set("Passport-Sdk-Version", "50561")
		request.Header.Set("X-VC-Bdturing-Sdk-Version", "3.7.2.cn")
		response, err := d.doHongguoVideoRequest(request, body, deviceID, installID)
		if err != nil {
			lastErr = err
			d.recordDiagnostic(diagnosticEvent{Level: "error", Event: "hongguo_multi_request", Source: sourceHongguo, DramaID: videoID, Attempt: attempt + 1, Message: publicError(err).Error()})
			var backoff *requestBackoff
			if errors.As(err, &backoff) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		_ = response.Body.Close()
		message := fmt.Sprintf("route=%s video=%s version=%s signatures=Gorgon,Helios,Medusa request_bytes=%d response_bytes=%d attempt=%d", hongguoMultiVideoPath, videoID, hongguoVideoAppVersion, len(body), len(content), attempt+1)
		if readErr != nil {
			lastErr = readErr
			d.recordHongguoAppResponse("error", "hongguo_multi_read", request, response, content, message)
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("红果批量取流 HTTP %d", response.StatusCode)
			d.recordHongguoAppResponse("error", "hongguo_multi_http", request, response, content, message)
			if response.StatusCode >= 400 && response.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		if len(content) == 0 || len(content) > providerMaxBodyBytes {
			lastErr = errors.New("红果批量取流未返回有效数据")
			d.recordHongguoAppResponse("error", "hongguo_multi_body", request, response, content, message)
			if len(content) > providerMaxBodyBytes {
				return nil, lastErr
			}
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		var result map[string]any
		if decoder.Decode(&result) != nil || result == nil || decoder.Decode(new(any)) != io.EOF {
			d.recordHongguoAppResponse("error", "hongguo_multi_json", request, response, content, message)
			return nil, errors.New("红果批量取流响应不是有效 JSON")
		}
		code := firstNonEmpty(mapString(result, "code", "Code", "status_code"), mapString(nestedMap(result, "BaseResp"), "StatusCode"))
		if code != "" && code != "0" {
			d.recordHongguoAppResponse("error", "hongguo_multi_code", request, response, content, message+" code="+truncate(code, 20))
			return nil, fmt.Errorf("红果批量取流接口拒绝请求（%s）", truncate(code, 20))
		}
		d.recordHongguoAppResponse("info", "hongguo_multi_ok", request, response, content, message+" keys="+diagnosticKeys(result, 12))
		return result, nil
	}
	return nil, lastErr
}

func (d *Downloader) doHongguoVideoRequest(request *http.Request, body []byte, deviceID, installID string) (*http.Response, error) {
	release := func() {}
	if d.limiter != nil {
		var err error
		release, err = d.limiter.acquire(request.Context(), request)
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	request = request.WithContext(ctx)
	nonce, err := newHongguoVideoNonce()
	if err == nil {
		now := time.Now()
		request.URL.RawQuery = hongguoVideoQuery(deviceID, installID, now, hongguoVideoIdentityCurrent)
		err = signHongguoVideoRequest(request, body, now, nonce)
	}
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	client := *d.client
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		return errors.New("红果取流接口重定向，已停止发送签名")
	}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	if d.limiter != nil {
		d.limiter.observe(request, response)
	}
	observeSourceResponse(ctx, response)
	response.Body = &limitedResponseBody{ReadCloser: response.Body, release: func() { cancel(); release() }}
	return response, nil
}

func hongguoVideoModel(result map[string]any, videoID string) (map[string]any, error) {
	data := nestedMap(result, "data")
	if data == nil {
		return nil, errors.New("红果批量取流缺少 data")
	}
	var entry map[string]any
	if value, found := data[videoID]; found {
		entry, _ = value.(map[string]any)
		if rows, ok := value.([]any); ok && len(rows) == 1 {
			entry, _ = rows[0].(map[string]any)
		}
		if entry == nil {
			return nil, errors.New("红果当前分集的播放信息无效")
		}
	} else if data["video_model"] != nil {
		entry = data
	} else {
		return nil, errors.New("红果批量取流未返回所请求的分集")
	}
	if id := mapString(entry, "video_id", "vid", "item_id"); id != "" && id != videoID {
		return nil, errors.New("红果批量取流分集身份不符")
	}
	model, _ := entry["video_model"].(map[string]any)
	if encoded, ok := entry["video_model"].(string); ok {
		if len(encoded) > providerMaxBodyBytes {
			return nil, errors.New("红果播放模型过大")
		}
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if decoder.Decode(&model) != nil || model == nil || decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("红果播放模型 JSON 无效")
		}
	}
	if len(model) == 0 {
		return nil, errors.New("红果当前分集没有播放模型")
	}
	return model, nil
}
