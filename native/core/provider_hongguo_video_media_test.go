package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHongguoVideoKeyAndEncryptedAddress(t *testing.T) {
	keyText := "0123456789abcdef0123456789abcdef"
	plain := []byte("0" + keyText)
	raw := make([]byte, len(plain)+1)
	even, odd := byte(246), byte(85)
	for i, v := range plain {
		previous := even
		if i&1 != 0 {
			previous = odd
		}
		current := previous ^ byte(int(v)+21+countHongguoBits(i))
		raw[i+1] = current
		if i&1 == 0 {
			even = current
		} else {
			odd = current
		}
	}
	raw[0] = raw[1] ^ raw[2]
	encodedKey := base64.RawURLEncoding.EncodeToString(raw)
	key, err := hongguoVideoContentKey(encodedKey)
	if err != nil || hex.EncodeToString(key) != keyText {
		t.Fatal("content key differs", err)
	}
	seed := []byte("synthetic-key-seed")
	constants, _ := hex.DecodeString("4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58")
	first := sha512.Sum512(seed)
	second := sha512.Sum512(append(first[:], constants...))
	block, _ := aes.NewCipher(second[:16])
	address := "https://media.example.test/main.mp4?token=synthetic"
	data := pkcs7Pad([]byte(address), 16)
	cipher.NewCBCEncrypter(block, second[16:32]).CryptBlocks(data, data)
	encoded := base64.RawURLEncoding.EncodeToString(append([]byte{0xa8, 0, 1, 0}, data...))
	got, err := hongguoDecodeMediaAddress(encoded, seed)
	if err != nil || got != address {
		t.Fatal("encrypted address differs", err)
	}
	model := map[string]any{"key_seed": base64.StdEncoding.EncodeToString(seed), "dynamic_video_list": []any{map[string]any{"main_url": encoded, "spade_a": encodedKey, "definition": "1080p", "video_meta": map[string]any{"codec_type": "h264"}}}}
	media, err := selectHongguoAppMedia(model)
	if err != nil || media.URL != address || media.Quality != 1080 || hex.EncodeToString(media.CENCKey) != keyText {
		t.Fatal("model fields were lost", err)
	}
	req, _ := http.NewRequest(http.MethodGet, address, nil)
	if media.credentials == nil || media.credentials.apply(req) != nil || req.UserAgent() != "com.phoenix.read/71332" {
		t.Fatal("media user agent lost")
	}
	if _, err := hongguoDecodeMediaAddress(encoded, nil); err == nil {
		t.Fatal("accepted encrypted address without seed")
	}
	if _, err := hongguoVideoContentKey("invalid"); err == nil {
		t.Fatal("accepted malformed content key")
	}
	if hex.EncodeToString(sha256SumHongguoTables()) != "059a54341a1f7e9b78243b144a79677331b44c7896d4b9dae77fcf0e6e1075e6" {
		t.Fatal("fixed table checksum mismatch")
	}
}

func sha256SumHongguoTables() []byte { value := sha256.Sum256(hongguoSignTables); return value[:] }
func countHongguoBits(value int) int {
	count := 0
	for value > 0 {
		count += value & 1
		value >>= 1
	}
	return count
}

func TestHongguoMultiVideoEmptyResponseAndCancellation(t *testing.T) {
	requests := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		return sourceFixtureResponse(request, http.StatusOK, ""), nil
	})
	d.cfg.Retries = 2
	if _, err := d.hongguoVideoModelRequest(context.Background(), "123"); err == nil || requests != 2 {
		t.Fatal("empty-response retries not bounded", requests, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.hongguoVideoModelRequest(ctx, "123"); err != context.Canceled || requests != 2 {
		t.Fatal("canceled request touched upstream", err)
	}
	preview := diagnosticBodyPreview([]byte(`{"token":"synthetic-secret","url":"https://media.test/private?signature=synthetic"}`))
	if strings.Contains(preview, "synthetic-secret") || strings.Contains(preview, "signature=") {
		t.Fatal("diagnostic exposed response values")
	}
}

func TestLiveHongguoVideoModelMetadataOnly(t *testing.T) {
	videoID := os.Getenv("CHECK_LIVE_HONGGUO_VIDEO")
	if videoID == "" {
		t.Skip("set CHECK_LIVE_HONGGUO_VIDEO for metadata-only diagnosis")
	}
	if !hongguoNumericID.MatchString(videoID) {
		t.Fatal("invalid video id")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.cfg.Retries = 1
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	media, err := engine.downloader.resolveHongguoAppMedia(ctx, videoID)
	for _, line := range engine.downloader.diagnosticLogTail(10) {
		t.Log(line)
	}
	if err != nil {
		t.Fatal(publicError(err))
	}
	t.Logf("metadata resolved: quality=%d routes=%d cenc=%t; no media requested", media.Quality, len(media.Variants), len(media.CENCKey) == 16)
}
