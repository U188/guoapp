package core

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const hongguoVideoAppVersion = "7.1.3.32"
const hongguoVideoAppUserAgent = "com.phoenix.read/71332 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

type hongguoVideoNonce struct {
	gorgon uint16
	helios uint32
	proto  uint32
	hash   uint32
	xm     uint32
	launch uint32
	pid    uint32
}

func newHongguoVideoNonce() (hongguoVideoNonce, error) {
	var data [26]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hongguoVideoNonce{}, errors.New("无法初始化红果取流签名")
	}
	return hongguoVideoNonce{
		gorgon: binary.LittleEndian.Uint16(data[:2]), helios: binary.LittleEndian.Uint32(data[2:6]),
		proto: binary.LittleEndian.Uint32(data[6:10]), hash: binary.LittleEndian.Uint32(data[10:14]),
		xm: binary.LittleEndian.Uint32(data[14:18]), launch: 100 + binary.LittleEndian.Uint32(data[18:22])%21,
		pid: 10001 + binary.LittleEndian.Uint32(data[22:26])%2000,
	}, nil
}

func signHongguoVideoRequest(request *http.Request, body []byte, now time.Time, nonce hongguoVideoNonce) error {
	stamp := uint32(now.Unix())
	bodyHash := md5.Sum(body)
	medusa, err := hongguoMedusa(request.URL.RawQuery, bodyHash, request.URL.Query().Get("device_id"), stamp, nonce)
	if err != nil {
		return err
	}
	request.Header.Set("X-Khronos", strconv.FormatUint(uint64(stamp), 10))
	request.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
	request.Header.Set("X-SS-STUB", strings.ToUpper(hex.EncodeToString(bodyHash[:])))
	request.Header.Set("X-Gorgon", hongguoVideoGorgon(request.URL.RawQuery, bodyHash, stamp, nonce.gorgon))
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], stamp)
	request.Header.Set("X-Ladon", base64.StdEncoding.EncodeToString(data[:]))
	binary.LittleEndian.PutUint32(data[:], stamp)
	request.Header.Set("X-Argus", base64.StdEncoding.EncodeToString(data[:]))
	request.Header.Set("X-Helios", hongguoVideoHelios(stamp, nonce.helios))
	request.Header.Set("X-Medusa", medusa)
	request.Header.Set("X-TT-DT", "")
	return nil
}

func hongguoVideoGorgon(query string, bodyHash [16]byte, stamp uint32, random uint16) string {
	queryHash := md5.Sum([]byte(query))
	data := make([]byte, 20)
	copy(data[:4], queryHash[:4])
	copy(data[4:8], bodyHash[:4])
	binary.LittleEndian.PutUint32(data[12:16], 67503104)
	binary.BigEndian.PutUint32(data[16:], stamp)
	key := [8]byte{0x4a, 0x40, 0x16, byte(random >> 8), 0x47, 0x6c, 1, byte(random)}
	var table [256]byte
	for i := range table {
		table[i] = byte(i)
	}
	j := byte(0)
	for i := range table {
		j += table[i] + key[i&7]
		table[i] = table[j]
	}
	j = 0
	for index := range data {
		i := byte(index + 1)
		x := table[i]
		j += x
		y := table[j]
		table[i] = y
		data[index] ^= table[y+y]
	}
	for i := range data {
		data[i] = ^(bits.Reverse8(data[(i+1)%20]^bits.RotateLeft8(data[i], 4)) ^ 20)
	}
	head := []byte{0x84, 4, byte(random), byte(random >> 8), 0x40, 1}
	return hex.EncodeToString(append(head, data...))
}

func hongguoVideoHelios(stamp, random uint32) string {
	seed := binary.LittleEndian.AppendUint32(nil, random)
	hash := md5.Sum(append(append([]byte(nil), seed...), []byte("8662")...))
	keys := []byte(hex.EncodeToString(hash[:]))
	first, second := binary.LittleEndian.Uint64(keys), binary.LittleEndian.Uint64(keys[8:])
	queue := []uint64{binary.LittleEndian.Uint64(keys[16:]), binary.LittleEndian.Uint64(keys[24:])}
	table := []uint64{first}
	for i := 0; i < 34; i++ {
		mixed := (bits.RotateLeft64(second, -8) + first) ^ uint64(i)
		queue = append(queue, mixed)
		first = mixed ^ bits.RotateLeft64(first, 3)
		table = append(table, first)
		second, queue = queue[0], queue[1:]
	}
	data := pkcs7Pad([]byte(strconv.FormatUint(uint64(stamp), 10)+"-1588093228-8662"), 16)
	for offset := 0; offset < len(data); offset += 16 {
		left, right := binary.LittleEndian.Uint64(data[offset:]), binary.LittleEndian.Uint64(data[offset+8:])
		for i := 0; i < 34; i++ {
			right = table[i] ^ (left + bits.RotateLeft64(right, -8))
			left = right ^ bits.RotateLeft64(left, 3)
		}
		binary.LittleEndian.PutUint64(data[offset:], left)
		binary.LittleEndian.PutUint64(data[offset+8:], right)
	}
	return base64.StdEncoding.EncodeToString(append(seed, data...))
}
