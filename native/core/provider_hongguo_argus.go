package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	hongguoLicenseID = 1611921764
	hongguoSignAID   = 8662
)

const (
	hongguoSimonZHI = 0x3DC94C3A
	hongguoSimonZLO = 0x046D678B
)

var (
	hongguoArgusSignKey = hongguoHexBytes("fc78e0a9657a0c748ce51559903ccf03510e51d3cff232d71343e88a321c5304")
	hongguoArgusAESKey  = hongguoHexBytes("d31e3718288a1027baab59f146a09a9c")
	hongguoArgusAESIV   = hongguoHexBytes("ea180a0336ed352fcd24e4d50018ae54")
)

func hongguoEncodeQuery(raw string) string {
	return strings.ReplaceAll(strings.ReplaceAll(raw, "+", "%20"), "%2A", "*")
}

func hongguoHexBytes(value string) []byte {
	data, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return data
}

func hongguoPKCS7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func hongguoSimonZBit(index int) uint64 {
	if index < 32 {
		return uint64((hongguoSimonZLO >> index) & 1)
	}
	return uint64((hongguoSimonZHI >> (index - 32)) & 1)
}

func hongguoSimonExpand(key [4]uint64) [72]uint64 {
	var expanded [72]uint64
	copy(expanded[:4], key[:])
	for index := 4; index < 72; index++ {
		temp := bits.RotateLeft64(expanded[index-1], -3)
		temp ^= expanded[index-3]
		temp ^= bits.RotateLeft64(temp, -1)
		expanded[index] = ^expanded[index-4] ^ temp ^ ((hongguoSimonZBit((index-4)%62) ^ 3) & 0xffffffff)
	}
	return expanded
}

func hongguoSimonEncrypt(left, right uint64, key [72]uint64) (uint64, uint64) {
	for index := 0; index < 72; index++ {
		previous := right
		right = left ^ (bits.RotateLeft64(right, 1) & bits.RotateLeft64(right, 8)) ^ bits.RotateLeft64(right, 2) ^ key[index]
		left = previous
	}
	return left, right
}

func hongguoSimonEncryptBlocks(data []byte) []byte {
	var key [4]uint64
	for index := 0; index < 4; index++ {
		key[index] = binary.LittleEndian.Uint64(hongguoArgusSignKey[index*8:])
	}
	expanded := hongguoSimonExpand(key)
	out := make([]byte, 0, len(data))
	for offset := 0; offset+16 <= len(data); offset += 16 {
		left, right := hongguoSimonEncrypt(binary.LittleEndian.Uint64(data[offset:]), binary.LittleEndian.Uint64(data[offset+8:]), expanded)
		var block [16]byte
		binary.LittleEndian.PutUint64(block[:], left)
		binary.LittleEndian.PutUint64(block[8:], right)
		out = append(out, block[:]...)
	}
	return out
}

type hongguoPBuf struct {
	data []byte
}

func (buffer *hongguoPBuf) varint(value uint64) {
	for value >= 0x80 {
		buffer.data = append(buffer.data, byte(value&0x7f)|0x80)
		value >>= 7
	}
	buffer.data = append(buffer.data, byte(value&0x7f))
}

func (buffer *hongguoPBuf) V(index int, value uint64) *hongguoPBuf {
	buffer.varint(uint64(index) << 3)
	buffer.varint(value & 0xffffffff)
	return buffer
}

func (buffer *hongguoPBuf) Raw(index int, data []byte) *hongguoPBuf {
	buffer.varint(uint64(index)<<3 | 2)
	buffer.varint(uint64(len(data)))
	buffer.data = append(buffer.data, data...)
	return buffer
}

func (buffer *hongguoPBuf) S(index int, text string) *hongguoPBuf {
	return buffer.Raw(index, []byte(text))
}

func (buffer *hongguoPBuf) PB(index int, sub *hongguoPBuf) *hongguoPBuf {
	return buffer.Raw(index, sub.data)
}

func hongguoQueryValue(query, name string) string {
	text := strings.TrimPrefix(query, "?")
	if at := strings.IndexByte(text, '#'); at >= 0 {
		text = text[:at]
	}
	for _, part := range strings.Split(text, "&") {
		pair := strings.SplitN(part, "=", 2)
		if pair[0] != name {
			continue
		}
		if len(pair) > 1 {
			return pair[1]
		}
		return ""
	}
	return ""
}

func hongguoArgusHash(value string) []byte {
	sum := hongguoSM3([]byte(value))
	if value == "" {
		sum = hongguoSM3(make([]byte, 16))
	}
	return append([]byte(nil), sum[:6]...)
}

func hongguoArgusBodyHash(stubHex string) []byte {
	if data, err := hex.DecodeString(strings.TrimSpace(stubHex)); err == nil && len(data) > 0 {
		sum := hongguoSM3(data)
		return append([]byte(nil), sum[:6]...)
	}
	sum := hongguoSM3(make([]byte, 16))
	return append([]byte(nil), sum[:6]...)
}

func hongguoArgusEncrypt(bean []byte) (string, error) {
	magic := []byte{0xf2, 0xf7, 0xfc, 0xff, 0xf2, 0xf7, 0xfc, 0xff}
	tail := []byte{0xea, 0xfb, 0x2c, 0xfe, 0x85, 0x68, 0x51, 0x91, 0x54}
	raw := append(append(append([]byte(nil), magic...), hongguoSimonEncryptBlocks(hongguoPKCS7Pad(bean, 16))...), tail...)
	body := append([]byte(nil), raw[:8]...)
	for index := 8; index < len(raw); index++ {
		body = append(body, raw[index]^magic[index%8])
	}
	for left, right := 0, len(body)-1; left < right; left, right = left+1, right-1 {
		body[left], body[right] = body[right], body[left]
	}
	body = append(body, 'a', 'o')
	block, err := aes.NewCipher(hongguoArgusAESKey)
	if err != nil {
		return "", err
	}
	padded := hongguoPKCS7Pad(body, aes.BlockSize)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, hongguoArgusAESIV).CryptBlocks(encrypted, padded)
	return base64.StdEncoding.EncodeToString(append([]byte{0xf2, 0x81}, encrypted...)), nil
}

func hongguoArgusSign(query, stubHex string, stamp int64, aid, licenseID int64, deviceID, installID string, randField int) (string, error) {
	if randField <= 0 {
		var data [4]byte
		if _, err := rand.Read(data[:]); err != nil {
			return "", err
		}
		randField = 1 + int(binary.BigEndian.Uint32(data[:])%2147483647)
	}
	bean := &hongguoPBuf{}
	bean.V(1, 1077940818).V(2, 2).V(3, uint64(randField))
	bean.S(4, strconv.FormatInt(aid, 10)).S(5, deviceID)
	bean.S(6, strconv.FormatInt(licenseID, 10)).S(7, hongguoQueryValue(query, "version_name"))
	bean.S(8, "v04.04.05-ov-android").V(9, 134744640).Raw(10, make([]byte, 8)).V(11, 0)
	bean.V(12, uint64(stamp*2))
	bean.Raw(13, hongguoArgusBodyHash(stubHex)).Raw(14, hongguoArgusHash(query))
	bean.PB(15, (&hongguoPBuf{}).V(1, 1).V(2, 1).V(3, 1).V(7, 0xC792ECCC))
	bean.S(16, installID).S(20, "none").V(21, 738)
	bean.PB(23, (&hongguoPBuf{}).S(1, "NX551J").V(2, 8196).V(4, 0x80E0D800))
	bean.V(25, 2)
	return hongguoArgusEncrypt(bean.data)
}

func hongguoLadonExpand(md5hex string) []byte {
	table := make([]byte, 288)
	copy(table, md5hex)
	load := func(offset int) uint64 { return binary.LittleEndian.Uint64(table[offset:]) }
	first, second := load(0), load(8)
	queue := []uint64{load(16), load(24)}
	for index := 0; index < 34; index++ {
		previous := first
		value := bits.RotateLeft64(second, -8) + previous
		value ^= uint64(index)
		queue = append(queue, value)
		value ^= bits.RotateLeft64(previous, 3)
		binary.LittleEndian.PutUint64(table[(index+1)*8:], value)
		first, second = value, queue[0]
		queue = queue[1:]
	}
	return table
}

func hongguoLadonEncrypt(stamp, licenseID, aid int64, random4 []byte) string {
	data := []byte(fmt.Sprintf("%d-%d-%d", stamp, licenseID, aid))
	keygen := append(append([]byte(nil), random4...), strconv.FormatInt(aid, 10)...)
	sum := md5.Sum(keygen)
	table := hongguoLadonExpand(hex.EncodeToString(sum[:]))
	padded := hongguoPKCS7Pad(data, 16)
	out := make([]byte, 0, len(padded))
	for offset := 0; offset < len(padded); offset += 16 {
		left := binary.LittleEndian.Uint64(padded[offset:])
		right := binary.LittleEndian.Uint64(padded[offset+8:])
		for index := 0; index < 34; index++ {
			right = left + bits.RotateLeft64(right, -8)
			right ^= binary.LittleEndian.Uint64(table[index*8:])
			left = right ^ bits.RotateLeft64(left, 3)
		}
		var block [16]byte
		binary.LittleEndian.PutUint64(block[:], left)
		binary.LittleEndian.PutUint64(block[8:], right)
		out = append(out, block[:]...)
	}
	return base64.StdEncoding.EncodeToString(append(append([]byte(nil), random4...), out...))
}

func signHongguoPlatform(request *http.Request, body []byte, now time.Time) {
	query := request.URL.RawQuery
	values := request.URL.Query()
	aid := int64(hongguoSignAID)
	if parsed, err := strconv.ParseInt(values.Get("aid"), 10, 64); err == nil && parsed > 0 {
		aid = parsed
	}
	stub := ""
	if len(body) > 0 {
		sum := md5.Sum(body)
		stub = hex.EncodeToString(sum[:])
	}
	var random4 [4]byte
	_, _ = rand.Read(random4[:])
	if argus, err := hongguoArgusSign(query, stub, now.Unix(), aid, hongguoLicenseID, values.Get("device_id"), values.Get("iid"), 0); err == nil {
		request.Header.Set("X-Argus", argus)
	}
	request.Header.Set("X-Ladon", hongguoLadonEncrypt(now.Unix(), hongguoLicenseID, aid, random4[:]))
}
