package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// xchinaSeal 用与站点相同的 OpenSSL Salted 格式封一个响应信封，
// 供测试验证解密路径（EVP_BytesToKey(MD5) + AES-256-CBC + PKCS#7）。
func xchinaSeal(t *testing.T, plain []byte) string {
	t.Helper()
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	derived := []byte{}
	var previous []byte
	for len(derived) < 48 {
		digest := md5.New()
		digest.Write(previous)
		digest.Write([]byte(xchinaPassphrase))
		digest.Write(salt)
		previous = digest.Sum(nil)
		derived = append(derived, previous...)
	}
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		t.Fatal(err)
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, derived[32:48]).CryptBlocks(out, padded)
	envelope := append([]byte("Salted__"), salt...)
	envelope = append(envelope, out...)
	encoded, _ := json.Marshal(map[string]string{"r": base64.StdEncoding.EncodeToString(envelope)})
	return string(encoded)
}

func TestXChinaEnvelopeRoundTrip(t *testing.T) {
	payload := map[string]any{"code": 0, "list": []any{map[string]any{"id": "abc123", "title": "测试视频"}}}
	plain, _ := json.Marshal(payload)
	sealed := xchinaSeal(t, plain)
	value, err := xchinaUnwrap([]byte(sealed))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := value.(map[string]any)
	rows := xchinaRows(row)
	if len(rows) != 1 || mapString(rows[0], "id") != "abc123" {
		t.Fatalf("unexpected payload %+v", value)
	}
}

func TestXChinaEnvelopeRejectsBroken(t *testing.T) {
	for _, body := range []string{`{"r":"not-base64!!"}`, `{"r":"` + base64.StdEncoding.EncodeToString([]byte("plain")) + `"}`, `<html>`} {
		if _, err := xchinaUnwrap([]byte(body)); err == nil {
			t.Fatalf("accepted invalid envelope %q", body)
		}
	}
}

// TestXChinaCatalogAndDetail 用合成响应验证目录/详情/取流的请求形状与解析。
func TestXChinaCatalogAndDetail(t *testing.T) {
	var paths []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/js") {
			return sourceFixtureResponse(request, 404, "{}"), nil
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal("business body is not json", err)
		}
		if body["app"] != xchinaAppName || body["uuid"] != xchinaPlaceholderUUID {
			t.Fatal("missing fixed fields", body)
		}
		if request.Header.Get("Origin") == "" || !strings.HasPrefix(request.Header.Get("Referer"), "http") {
			t.Fatal("missing origin/referer", request.Header)
		}
		route, _ := body["url"].(string)
		route = strings.TrimSpace(route)
		paths = append(paths, route)
		var payload any
		switch {
		case strings.HasPrefix(route, "/sevenVideos/"):
			payload = map[string]any{"id": "abc123", "title": "测试视频", "duration": "125",
				"durationStr": "02:05", "time": "2026-10-01 12:00:00", "keywords_gem": "标签",
				"m3u8s": []any{"https://media.example.com/hls/abc123/index.m3u8"}}
		case strings.HasPrefix(route, "/sevenVideos"):
			payload = map[string]any{"list": []any{
				map[string]any{"id": "abc123", "title": "测试视频", "duration": "125"},
				map[string]any{"id": "def456", "title": "第二个", "duration": "60"},
			}}
		default:
			payload = map[string]any{"list": []any{}}
		}
		plain, _ := json.Marshal(payload)
		return sourceFixtureResponse(request, 200, xchinaSeal(t, plain)), nil
	})
	items, more, err := d.fetchXChinaCatalogPage(context.Background(), 1, "recent", "")
	if err != nil || len(items) != 2 || more {
		t.Fatalf("catalog failed items=%d more=%t err=%v", len(items), more, err)
	}
	if items[0].ID != "xchina:abc123" {
		t.Fatalf("bad drama id %s", items[0].ID)
	}
	if items[0].Source != sourceXChina || items[0].SourceID != "abc123" {
		t.Fatalf("bad drama source %+v", items[0])
	}
	drama, chapters, err := d.fetchXChinaDetail(context.Background(), "abc123")
	if err != nil || len(chapters) != 1 {
		t.Fatalf("detail failed %v %v", drama, err)
	}
	if chapters[0].ID != providerChapterID(sourceXChina, "abc123", "main") || chapters[0].Source != sourceXChina {
		t.Fatalf("bad chapter %+v", chapters[0])
	}
	if len(paths) < 2 || !strings.HasPrefix(paths[0], "/sevenVideos") {
		t.Fatal("wrong route sequence", paths)
	}
}

func TestXChinaCategoryValidation(t *testing.T) {
	if !validXChinaCategory("recent") || validXChinaCategory("nope") || validXChinaCategory("") {
		t.Fatal("category validation broken")
	}
	if !validXChinaSort("") || !validXChinaSort("views") || validXChinaSort("bogus") {
		t.Fatal("sort validation broken")
	}
	if !validXChinaID("abc-_123.4") || validXChinaID("../etc") || validXChinaID("") {
		t.Fatal("id validation broken")
	}
	if !validCatalogSort(sourceXChina, "latest") || validCatalogSort(sourceHuangju, "views") {
		t.Fatal("catalog sort validation broken")
	}
	if _, err := url.Parse(xchinaBaseURL); err != nil {
		t.Fatal(err)
	}
}
