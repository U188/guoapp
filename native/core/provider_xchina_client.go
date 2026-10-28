package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// xchina 站点是 Angular/Ionic SPA，页面里没有可爬数据：全部数据走
// POST /js，响应体是 {"r": "<OpenSSL Salted AES-256-CBC base64>"}。
// 三个硬性条件缺一即 403：
//  1. uuid 必须为 "novalue" 或 14 位纯数字（空值、32 位 hex 一律拒绝）
//  2. Origin / Referer 必须带站点域名
//  3. 未授权请求需保留 Authorization: Bearer <token>（可为空串）
const (
	xchinaBaseURL   = "https://xchina.tube"
	xchinaAppName   = "xchina.tube"
	xchinaVersion   = "260901"
	xchinaPassphrase = "xxx"
	xchinaPlaceholderUUID = "novalue"
	xchinaJSONPath  = "/js"
	xchinaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	xchinaAPIHosts = []string{
		"https://v2.cdn199.com",
		"https://v2.kekecdn.net",
		"https://v2.luchu.org",
		"https://v2.madou.ws",
		"https://v2.papapa.biz",
		"https://v2.xiaoshuo.info",
		"https://v2.xiaoshuo.la",
	}
	xchinaImageHosts = []string{
		"https://tp.xiaoxx.com",
		"https://tp.losers.app",
		"https://tp.pppsuccess.com",
		"https://tp.365novel.com",
		"https://tp.family4k.com",
		"https://tp.helloye.com",
	}
)

type xchinaAPIClient struct {
	downloader *Downloader
	mu         sync.Mutex
	host       string
	image      string
	token      string
	uuid       string
	cached     map[string]map[string]any
}

type xchinaBlockedError struct{}

func (xchinaBlockedError) Error() string { return "站源限流中，已自动退避" }

// xchinaTransientError 表示可以安全重试的传输层失败（连接、超时、5xx）。
// 站点按 IP 随机限流，约四成请求会被拒，因此网络错误和限流一样需要退避重试；
// 而 4xx/解密失败属于确定性错误，立即换线路，避免无谓等待。
type xchinaTransientError struct{ err error }

func (e xchinaTransientError) Error() string { return e.err.Error() }
func (e xchinaTransientError) Unwrap() error { return e.err }

func xchinaRetryable(err error) bool {
	var blocked xchinaBlockedError
	if errors.As(err, &blocked) {
		return true
	}
	var transient xchinaTransientError
	return errors.As(err, &transient)
}

func (d *Downloader) xchinaClient() *xchinaAPIClient {
	d.xchinaOnce.Do(func() {
		d.xchina = &xchinaAPIClient{
			downloader: d,
			host:       strings.TrimRight(firstNonEmpty(d.cfg.XChinaAPIURL, xchinaAPIHosts[0]), "/"),
			image:      strings.TrimRight(firstNonEmpty(d.cfg.XChinaImageURL, xchinaImageHosts[0]), "/"),
			token:      d.cfg.XChinaToken,
			uuid:       xchinaPlaceholderUUID,
			cached:     map[string]map[string]any{},
		}
	})
	return d.xchina
}

func (client *xchinaAPIClient) siteURL() string {
	return strings.TrimRight(client.downloader.providerBaseURL(sourceXChina), "/")
}

// evpBytesToKey 复刻 OpenSSL EVP_BytesToKey(MD5, 1 轮)，
// 生成 AES-256 的 key(32B) 与 iv(16B)。
func xchinaEVPBytesToKey(passphrase, salt []byte) ([]byte, []byte) {
	derived := make([]byte, 0, 48)
	var previous []byte
	for len(derived) < 48 {
		digest := md5.New()
		digest.Write(previous)
		digest.Write(passphrase)
		digest.Write(salt)
		previous = digest.Sum(nil)
		derived = append(derived, previous...)
	}
	return derived[:32], derived[32:48]
}

// xchinaDecryptEnvelope 解开 {"r": "<base64>"}，还原成内层 JSON 文本。
// 外层的 "Salted__" 头后 8 字节是 salt，正文是 PKCS#7 填充的 CBC 密文。
func xchinaDecryptEnvelope(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(encoded))
	}
	if err != nil {
		return nil, errors.New("站源响应编码无法识别")
	}
	if len(raw) < 32 || !bytes.HasPrefix(raw, []byte("Salted__")) {
		return nil, errors.New("站源响应不是加密信封")
	}
	key, iv := xchinaEVPBytesToKey([]byte(xchinaPassphrase), raw[8:16])
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	cipherText := raw[16:]
	if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return nil, errors.New("站源响应密文长度异常")
	}
	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, cipherText)
	if padding := int(plain[len(plain)-1]); padding < 1 || padding > aes.BlockSize || padding > len(plain) {
		return nil, errors.New("站源响应填充无效")
	} else {
		plain = plain[:len(plain)-padding]
	}
	return plain, nil
}

func xchinaUnwrap(body []byte) (any, error) {
	var envelope map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("站源响应不是 JSON")
	}
	if encoded, ok := envelope["r"]; ok {
		var text string
		if err := json.Unmarshal(encoded, &text); err != nil {
			return nil, errors.New("站源响应信封格式无效")
		}
		plain, err := xchinaDecryptEnvelope(text)
		if err != nil {
			return nil, err
		}
		var payload any
		inner := json.NewDecoder(bytes.NewReader(plain))
		inner.UseNumber()
		if err := inner.Decode(&payload); err != nil {
			return nil, errors.New("站源响应解密后不是 JSON")
		}
		return payload, nil
	}
	if _, blocked := envelope["blocked"]; blocked {
		return nil, xchinaBlockedError{}
	}
	var payload any
	inner := json.NewDecoder(bytes.NewReader(body))
	inner.UseNumber()
	if err := inner.Decode(&payload); err != nil {
		return nil, errors.New("站源响应格式无效")
	}
	return payload, nil
}

// post 发送一次业务请求。payload 会与固定字段合并后作为 JSON body。
func (client *xchinaAPIClient) post(ctx context.Context, path string, payload map[string]any) (any, error) {
	body := map[string]any{
		"app":          xchinaAppName,
		"isStandalone": false,
		"theLink":      "novaluenull",
		"uuid":         client.uuid,
		"url":          path,
		"version":      xchinaVersion,
	}
	for key, value := range payload {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	host := client.host
	client.mu.Unlock()
	address := host + xchinaJSONPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("站源接口地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+client.token)
	site := client.siteURL()
	request.Header.Set("Origin", site)
	request.Header.Set("Referer", site+"/")
	request.Header.Set("User-Agent", xchinaUserAgent)
	request.Header.Set("Accept", "*/*")
	downloader := client.downloader
	if downloader.limiter != nil {
		release, err := downloader.limiter.acquire(ctx, request)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	transport := *downloader.client
	transport.Jar = nil
	previousRedirect := transport.CheckRedirect
	transport.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || providerMediaOrigin(next.URL) != providerMediaOrigin(request.URL) {
			return errors.New("站源接口重定向地址异常")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := transport.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, xchinaTransientError{fmt.Errorf("站源连接失败：%w", publicError(err))}
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if readErr != nil || len(raw) > 16<<20 {
		return nil, errors.New("站源响应读取失败")
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return nil, xchinaBlockedError{}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err := fmt.Errorf("站源接口返回 HTTP %d", response.StatusCode)
		if response.StatusCode >= 500 {
			return nil, xchinaTransientError{err}
		}
		return nil, err
	}
	return xchinaUnwrap(raw)
}

// call 在多条 API 线路间故障切换，并对 {"blocked":true} 做退避重试。
func (client *xchinaAPIClient) call(ctx context.Context, path string, payload map[string]any) (any, error) {
	client.mu.Lock()
	hosts := []string{client.host}
	for _, candidate := range xchinaAPIHosts {
		if candidate != client.host {
			hosts = append(hosts, candidate)
		}
	}
	client.mu.Unlock()
	var lastErr error
	for _, host := range hosts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client.mu.Lock()
		client.host = host
		client.mu.Unlock()
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-time.After(time.Duration(attempt)*700*time.Millisecond + 300*time.Millisecond):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			value, err := client.post(ctx, path, payload)
			if err == nil {
				return value, nil
			}
			lastErr = err
			if !xchinaRetryable(err) {
				break
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("站源暂未返回数据")
	}
	return nil, lastErr
}

func (client *xchinaAPIClient) imageURL(id string) string {
	client.mu.Lock()
	base := client.image
	client.mu.Unlock()
	return strings.TrimRight(base, "/") + "/" + url.PathEscape(id) + ".jpg"
}

func (client *xchinaAPIClient) remember(videoID string, row map[string]any) {
	if videoID == "" || len(row) == 0 {
		return
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.cached == nil || len(client.cached) >= 300 {
		client.cached = map[string]map[string]any{}
	}
	client.cached[videoID] = row
}

func (client *xchinaAPIClient) recall(videoID string) map[string]any {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.cached[videoID]
}
