package core

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
)

type hongguoProtoField struct {
	id     uint64
	kind   byte
	number int64
	text   string
	data   []byte
	value  float32
}

func hongguoProto(fields ...hongguoProtoField) []byte {
	var output []byte
	for _, field := range fields {
		switch field.kind {
		case 'i':
			if field.number == 0 {
				continue
			}
			output = binary.AppendUvarint(output, field.id<<3)
			var value uint64
			if field.number < 0 {
				value = uint64(-(field.number+1))*2 + 1
			} else {
				value = uint64(field.number) * 2
			}
			output = binary.AppendUvarint(output, value)
		case 'f':
			if field.value == 0 {
				continue
			}
			output = binary.AppendUvarint(output, field.id<<3|5)
			output = binary.LittleEndian.AppendUint32(output, math.Float32bits(field.value))
		case 's', 'b':
			data := field.data
			if field.kind == 's' {
				data = []byte(field.text)
			}
			if len(data) == 0 {
				continue
			}
			output = binary.AppendUvarint(output, field.id<<3|2)
			output = binary.AppendUvarint(output, uint64(len(data)))
			output = append(output, data...)
		}
	}
	return output
}

func hongguoProtoInt(id uint64, value int64) hongguoProtoField {
	return hongguoProtoField{id: id, kind: 'i', number: value}
}
func hongguoProtoString(id uint64, value string) hongguoProtoField {
	return hongguoProtoField{id: id, kind: 's', text: value}
}
func hongguoProtoBytes(id uint64, value []byte) hongguoProtoField {
	return hongguoProtoField{id: id, kind: 'b', data: value}
}
func hongguoProtoFloat(id uint64, value float32) hongguoProtoField {
	return hongguoProtoField{id: id, kind: 'f', value: value}
}

func hongguoMedusaProto(query string, body [16]byte, deviceID string, stamp uint32, nonce hongguoVideoNonce) ([]byte, [32]byte, []byte, error) {
	queryHash := hongguoSM3([]byte(query))
	bodySignature, err := hongguoSignatureHash(queryHash, body, stamp)
	if err != nil {
		return nil, queryHash, nil, err
	}
	combined := append(append([]byte(query), body[:]...), []byte("none")...)
	combinedHash := hongguoSM3(combined)
	device := hongguoProto(
		hongguoProtoInt(1, 1), hongguoProtoInt(2, 2), hongguoProtoString(3, "8662"), hongguoProtoString(4, deviceID),
		hongguoProtoString(6, "!noperm!"), hongguoProtoInt(7, -888888), hongguoProtoInt(8, -888888),
		hongguoProtoInt(9, 3), hongguoProtoInt(10, -888888), hongguoProtoString(11, "!notset!"),
		hongguoProtoString(12, "Asia/Shanghai,8"), hongguoProtoString(13, "zh_CN"), hongguoProtoInt(14, 4),
		hongguoProtoFloat(16, 255.24993896484375), hongguoProtoFloat(17, 35.58599090576172),
		hongguoProtoFloat(18, 3.467449188232422), hongguoProtoFloat(19, 3.467449188232422),
		hongguoProtoFloat(20, 255.1754913330078), hongguoProtoFloat(21, 42.17544174194336),
		hongguoProtoString(22, "16"), hongguoProtoInt(23, 41), hongguoProtoInt(24, 36),
		hongguoProtoInt(25, 1728388016635), hongguoProtoInt(26, 1728388016635),
		hongguoProtoInt(27, 1728388016635), hongguoProtoInt(28, 1728388016637), hongguoProtoInt(29, -1),
		hongguoProtoString(30, "25053RT47C"), hongguoProtoString(31, "Redmi"), hongguoProtoString(32, "25053RT47C"),
		hongguoProtoString(33, "25053RT47C"), hongguoProtoString(34, "Xiaomi"),
		hongguoProtoString(35, "Redmi"), hongguoProtoString(36, "Redmi"), hongguoProtoInt(38, 31),
	)
	report := hongguoProto(hongguoProtoInt(1, int64(stamp)), hongguoProtoInt(2, -2), hongguoProtoInt(4, 200))
	environment := hongguoProto(
		hongguoProtoInt(1, int64(nonce.launch)), hongguoProtoInt(2, 146331399), hongguoProtoInt(3, 146331396),
		hongguoProtoInt(5, 7), hongguoProtoString(6, "v04.06.04.03-bugfix"), hongguoProtoInt(7, int64(nonce.pid)),
		hongguoProtoBytes(12, device), hongguoProtoBytes(13, report), hongguoProtoString(14, hongguoVideoAppVersion),
	)
	request := hongguoProto(hongguoProtoInt(1, 111), hongguoProtoInt(2, 10), hongguoProtoInt(3, 694367), hongguoProtoInt(5, 586952199))
	magic := []byte{0xf7, 0xe8, 0x5f, 0xfa, 0xd7, 0xd7, 0xdc, 0x3b, 0xd6, 0x2a, 0xc8, 0x70, 0x57, 0xcf, 0x61, 0x18}
	seed := binary.LittleEndian.AppendUint64(nil, 320)
	proto := hongguoProto(
		hongguoProtoBytes(1, magic), hongguoProtoInt(2, 3), hongguoProtoInt(3, int64(nonce.proto)),
		hongguoProtoString(4, "8662"), hongguoProtoString(5, deviceID), hongguoProtoString(6, "1588093228"),
		hongguoProtoString(7, hongguoVideoAppVersion), hongguoProtoString(8, "v04.06.04-ml-android"),
		hongguoProtoInt(9, 67503104), hongguoProtoBytes(10, seed), hongguoProtoInt(12, int64(stamp)),
		hongguoProtoBytes(13, bodySignature), hongguoProtoBytes(14, queryHash[:6]), hongguoProtoBytes(15, request),
		hongguoProtoInt(17, int64(stamp)), hongguoProtoBytes(19, combinedHash[:]), hongguoProtoString(20, "none"),
		hongguoProtoInt(21, 312), hongguoProtoBytes(23, environment),
		hongguoProtoString(24, `{"cmr":16777216,"cmr2":16777216,"un_h":1879194040,"vpn":0,"kd":0,"fkd":3672518972,"pd":-1872573247,"dyn":"","do":0,"tk":true}`),
	)
	return proto, queryHash, bodySignature, nil
}

func hongguoMedusa(query string, body [16]byte, deviceID string, stamp uint32, nonce hongguoVideoNonce) (string, error) {
	if len(hongguoSignTables) != 83536 {
		return "", errors.New("红果签名固定表缺失")
	}
	proto, queryHash, bodySignature, err := hongguoMedusaProto(query, body, deviceID, stamp, nonce)
	if err != nil {
		return "", err
	}
	signKey := []byte{0x8e, 0xbd, 0xfa, 0x38, 0x06, 0xec, 0xc5, 0xce, 0xe7, 0x94, 0x23, 0xe6, 0x02, 0x9e, 0xd8, 0x25, 0x40, 0xbc, 0x22, 0x18, 0xbb, 0x7e, 0xae, 0xf7, 0x1c, 0xb6, 0x91, 0xf7, 0xaa, 0x8a, 0xa2, 0xf5}
	material := append(append(append([]byte(nil), signKey...), binary.LittleEndian.AppendUint32(nil, nonce.hash)...), signKey...)
	key := hongguoSM3(material)
	d1 := nonce.hash >> 16 & 255
	seed := ^((d1<<11 | nonce.hash>>24) ^ d1>>5 ^ d1)
	mixed := make([]byte, len(proto))
	for i, v := range proto {
		pos := i * 4 & 28
		a, b := key[pos], key[pos+1]
		n := bits.RotateLeft8(v, 4) + a
		n ^= b
		n = bits.RotateLeft8(^n, 3)
		n = (n + b) ^ a
		mixed[len(proto)-i-1] = ^n
	}
	last := mixed[len(mixed)-1] ^ mixed[len(mixed)-2]
	first := mixed[0]
	mixed[0] = ^last + mixed[0]
	mixed[1] = (mixed[0] ^ mixed[len(mixed)-1] ^ 254) + mixed[1]
	mixed[2] += ((last - first) ^ bits.RotateLeft8(mixed[1], 3) ^ 2)
	for i := 0; i < len(mixed)-4; i++ {
		temp := bits.RotateLeft8(mixed[i+2], 3) ^ mixed[i+1] ^ byte(i+3)
		mixed[i+3] = ^temp + mixed[i+3]
	}
	mixed[len(mixed)-1] ^= mixed[len(mixed)-2]
	sum := byte(0)
	for _, v := range mixed[1:] {
		sum += v
	}
	mixed[0] = (mixed[0] ^ mixed[1]) + sum
	mixed = append(binary.LittleEndian.AppendUint64(nil, 320), mixed...)
	for left, right := 0, len(mixed)-1; left < right; left, right = left+1, right-1 {
		mixed[left], mixed[right] = mixed[right], mixed[left]
	}
	for i := range mixed {
		mixed[i] ^= byte(seed >> uint((^i&3)*8))
	}
	check := uint32(queryHash[0]&63)<<14 | 0x18000001 | uint32(bodySignature[0]&63)<<8
	container := append([]byte{0x35}, binary.LittleEndian.AppendUint32(nil, nonce.xm)...)
	container = binary.LittleEndian.AppendUint32(container, check)
	container = append(container, mixed...)
	container = append(container, byte(nonce.hash>>16), byte(nonce.hash>>24))
	encrypted, err := hongguoAESV3(container, stamp)
	if err != nil {
		return "", err
	}
	version := []uint32{3, 0xfa5fe8f7, 0x3bdcd7d7, 0x70c82ad6, 0x1861cf57}
	var result []byte
	for _, word := range version {
		result = binary.LittleEndian.AppendUint32(result, word^stamp)
	}
	result = append(result, byte(nonce.hash), byte(nonce.hash>>8), 0, 1)
	return base64.StdEncoding.EncodeToString(append(result, encrypted...)), nil
}
