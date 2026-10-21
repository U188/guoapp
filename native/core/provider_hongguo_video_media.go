package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"strings"
)

func hongguoMediaKeySeed(model map[string]any, depth int) []byte {
	if depth > 4 {
		return nil
	}
	if value := mapString(model, "key_seed"); value != "" && len(value) <= 4096 {
		if data, err := decodeHongguoBase64(value); err == nil {
			return data
		}
	}
	for _, name := range []string{"dynamic_video", "video_info", "data"} {
		if data := hongguoMediaKeySeed(nestedMap(model, name), depth+1); len(data) > 0 {
			return data
		}
	}
	return nil
}

func hongguoDecodeMediaAddress(value string, seed []byte) (string, error) {
	value = strings.TrimSpace(value)
	if isProviderHTTPMediaURL(value) {
		return value, nil
	}
	raw, err := decodeHongguoBase64(value)
	if err != nil {
		return "", errors.New("红果媒体地址编码无效")
	}
	if isProviderHTTPMediaURL(strings.TrimSpace(string(raw))) {
		return strings.TrimSpace(string(raw)), nil
	}
	if len(raw) < 20 || raw[0] != 0xa8 || raw[2] != 1 || raw[3] != 0 || len(seed) == 0 {
		return "", errors.New("红果加密媒体地址缺少有效 key_seed")
	}
	constants, _ := hex.DecodeString("4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58")
	first := sha512.Sum512(seed)
	second := sha512.Sum512(append(first[:], constants...))
	block, err := aes.NewCipher(second[:16])
	if err != nil {
		return "", err
	}
	data := append([]byte(nil), raw[4:4+(len(raw)-4)/16*16]...)
	cipher.NewCBCDecrypter(block, second[16:32]).CryptBlocks(data, data)
	data, err = pkcs7Unpad(data, 16)
	if err != nil {
		return "", errors.New("红果加密媒体地址填充无效")
	}
	address := strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
	if !isProviderHTTPMediaURL(address) {
		return "", errors.New("红果解码后未返回 HTTP 媒体地址")
	}
	return address, nil
}

func hongguoModelAddresses(info map[string]any, seed []byte) []string {
	var addresses []string
	seen := map[string]bool{}
	var visit func(any, int)
	visit = func(value any, depth int) {
		if depth > 4 || len(addresses) >= 32 {
			return
		}
		switch value := value.(type) {
		case string:
			if len(value) > 8192 {
				return
			}
			if address, err := hongguoDecodeMediaAddress(value, seed); err == nil && !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		case []any:
			for _, entry := range value {
				visit(entry, depth+1)
			}
		case map[string]any:
			for _, name := range []string{"url", "uri", "src", "download_url", "main_url", "backup_url", "backup_url_1", "play_addr", "url_list"} {
				visit(value[name], depth+1)
			}
		}
	}
	for _, name := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list", "play_addr", "url"} {
		visit(info[name], 0)
	}
	return addresses
}
