package core

import (
	"crypto/aes"
	"encoding/hex"
	"errors"
	"math/bits"
	"strings"
)

func hongguoVideoContentKey(value string) ([]byte, error) {
	if len(value) > 1024 {
		return nil, errors.New("红果媒体密钥数据过长")
	}
	raw, err := decodeHongguoBase64(value)
	if err != nil || len(raw) < 3 {
		return nil, errors.New("红果媒体密钥编码无效")
	}
	length := len(raw) - int(raw[0]^raw[1]^raw[2]) + 47
	if length <= 0 || 1+length > len(raw) {
		length = len(raw) - 1
	}
	if length < 33 {
		return nil, errors.New("红果媒体密钥结构无效")
	}
	decoded := make([]byte, length)
	even, odd := byte(246), byte(85)
	for i, current := range raw[1 : 1+length] {
		previous := even
		if i&1 == 0 {
			even = current
		} else {
			previous = odd
			odd = current
		}
		decoded[i] = byte(int(previous^current) - 21 - bits.OnesCount(uint(i)))
	}
	key, err := hex.DecodeString(string(decoded[1:33]))
	if err != nil || len(key) != aes.BlockSize {
		return nil, errors.New("红果媒体密钥不是有效 AES-128 材料")
	}
	return key, nil
}

func hongguoResponseKind(content []byte) string {
	text := strings.TrimSpace(string(content))
	if text == "" {
		return "empty"
	}
	if strings.HasPrefix(text, "<") {
		return "html"
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return "json-like"
	}
	return "other"
}
