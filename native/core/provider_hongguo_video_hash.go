package core

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"math/bits"
)

//go:embed hongguo_sign_tables.bin
var hongguoSignTables []byte

func hongguoHashIV(iv uint32, data []byte) uint32 {
	for i, v := range data {
		if i&1 == 0 {
			iv = iv>>4 ^ iv ^ iv<<6 ^ uint32(v)
		} else {
			iv = ^(iv>>7 ^ iv ^ (uint32(v) | iv<<12))
		}
	}
	return iv
}

func hongguoSignatureHash(query [32]byte, body [16]byte, stamp uint32) ([]byte, error) {
	ts := binary.LittleEndian.AppendUint32(nil, stamp)
	iv := hongguoHashIV(hongguoHashIV(hongguoHashIV(0x20230928, query[:]), body[:]), ts)
	group := ((iv & 15) * 171) >> 9
	switch (iv & 15) - group*3 {
	case 0:
		return hongguoHashBranchZero(int(group), stamp, query, body, ts), nil
	case 1:
		return hongguoHashBranchOne(int(group), stamp, query, body, ts), nil
	case 2:
		return hongguoHashBranchTwo(iv, stamp, query, body, ts), nil
	}
	return nil, errors.New("红果签名哈希分支无效")
}

func hongguoHashChecksum(data []byte) uint32 {
	value := uint32(0x20220420)
	for i := 0; i < 12; i++ {
		var temp uint32
		if i&1 == 0 {
			temp = value>>3 ^ value
			value = uint32(data[i]) ^ value<<7
		} else {
			temp = value>>5 ^ value
			value = ^(uint32(data[i]) | value<<11)
		}
		value ^= temp
	}
	return (value | 4) ^ 0x1000000
}

func hongguoHashBranchTwo(iv, stamp uint32, query [32]byte, body [16]byte, ts []byte) []byte {
	n := (iv & 13) * 86
	group := int((n >> 15 & 255) + (n >> 8 & 255))
	seeds := [...]uint32{0x8980f29b, 0xeb549c7f, 0xb08726db, 0xd40cb5e6, 0xe8f559e4}
	count := seeds[group] + stamp
	shift := (count + 5) & 7
	data := append(append(append([]byte(nil), query[:]...), body[:]...), ts...)
	for _, v := range []byte{0x84, 0x96, 0x77, 0x9d, 0xd4, 0x15, 0x0b, 0xf8} {
		data = append(data, byte((uint16(v)*257)>>shift))
	}
	data = append(data, 0xa0, 1, 0, 0)
	orders := hongguoSignTables[1280+group*64 : 1280+(group+1)*64]
	var table [64]uint32
	for i := range table {
		table[i] = bits.RotateLeft32(binary.LittleEndian.Uint32(hongguoSignTables[1024+i*4:]), -int(byte(count+1)))
	}
	start := [4]uint32{0x79e0f2fb, 0xc8b52570, 0xebc2f8cd, 0x7c104d93}
	end := [4]uint32{0x19be4866, 0xe85986b4, 0xe19b326e, 0x71d1d7d4}
	for i := range start {
		start[i] = bits.RotateLeft32(start[i], -int(byte(count)))
		end[i] = bits.RotateLeft32(end[i], -int(byte(count+6)))
	}
	state := start
	shifts := [4][4]int{{7, 12, 17, 22}, {5, 9, 14, 20}, {4, 11, 16, 23}, {6, 10, 15, 21}}
	for i := 0; i < 64; i++ {
		a := (4 - i%4) % 4
		b, c, d := (a+1)%4, (a+2)%4, (a+3)%4
		var mixed uint32
		switch i / 16 {
		case 0:
			mixed = (state[b] & state[c]) | (^state[b] & state[d])
		case 1:
			mixed = (state[b] & state[d]) | (state[c] &^ state[d])
		case 2:
			mixed = state[b] ^ state[c] ^ state[d]
		case 3:
			mixed = state[c] ^ (state[b] | ^state[d])
		}
		word := binary.LittleEndian.Uint32(data[int(orders[i])*4:])
		state[a] = state[b] + bits.RotateLeft32(state[a]+mixed+word+table[i], shifts[i/16][i%4])
	}
	var result []byte
	for i := range state {
		result = binary.LittleEndian.AppendUint32(result, (start[i]+state[i])^end[i])
	}
	return binary.LittleEndian.AppendUint32(result, hongguoHashChecksum(result))
}

func hongguoHashBranchZero(group int, stamp uint32, query [32]byte, body [16]byte, ts []byte) []byte {
	seeds := [...]uint32{0xc4a78580, 0xb3c0fd39, 0xc58c5686, 0xc9aa3ba7, 0xf5a7adf2, 0x963c2ed1}
	base := seeds[group]
	count := base + stamp
	table := [64]uint32{0xebb64faf, 0x07aadcc2, 0xcf3187bf, 0xe01138ff, 0x6d0bfcff, 0x5a30a3be, 0xb41ad638, 0x34180eb8, 0xf233eb6f, 0xb1a584cc, 0xccc30dc7, 0x47d1db51, 0xd55653de, 0x70a84fa1, 0x57473c12, 0xf76f0288, 0x2c077f0a, 0xda0dcad0, 0xfbb86f6c, 0xfdc4cf00, 0x688a020d, 0xe676c6a6, 0x8cd6338b, 0x1a3c8d0e, 0xcce8b06b, 0x6ad0ed0b, 0xa0522717, 0xdc71ac83, 0x2285db71, 0xd5b4dda6, 0x736f8650, 0x6560306c, 0x617ce2a6, 0xe423417e, 0x0a40e143, 0x544e4032, 0x88dffb2a, 0x716c1ae0, 0x4c467a88, 0x05b23bb3, 0xe1d0b866, 0xbaa3dcb8, 0xae3374d3, 0xc3381a50, 0x1702f75b, 0xfe6da368, 0xf0b4cf48, 0x4e0ffbb8, 0x72aad10d, 0x26c53a3d, 0xf2bce0f6, 0xb4557581, 0x4a257fdd, 0x8c3182a2, 0xab0b3b86, 0x3d5dfb14, 0x4f103634, 0xd37b52d7, 0x444eff16, 0xeb0a33d1, 0x6ca86f6e, 0x00284ba7, 0x08387cfa, 0x5fb37586}
	for i := range table {
		table[i] = bits.RotateLeft32(table[i], -int(byte(count+1)))
	}
	data := append(append(append([]byte(nil), query[:]...), body[:]...), ts...)
	for _, v := range []byte{0xfa, 0x45, 0x61, 0xd7} {
		data = append(data, byte((uint16(v)*257)>>((count+2)&7)))
	}
	data = append(data, 0, 0, 0, 0, 0, 0, 1, 0xa0)
	var words [128]uint32
	for i := 0; i < 16; i++ {
		words[i] = binary.BigEndian.Uint32(data[i*4:])
	}
	for i := 0; i < 112; i++ {
		one, fourteen := words[i+1], words[i+14]
		words[i+16] = words[i] + words[i+9] + (bits.RotateLeft32(one, 14) ^ bits.RotateLeft32(one, 25) ^ one>>3) + (bits.RotateLeft32(fourteen, 13) ^ bits.RotateLeft32(fourteen, 15) ^ fourteen>>10)
	}
	initial := [8]uint32{0x7aba4fc8, 0x67166507, 0x6403fa00, 0x340f512f, 984304912, 3005047866, 2874125293, 2152413264}
	for i := range initial {
		initial[i] = bits.RotateLeft32(initial[i], -int(byte(count)))
	}
	orders := [6][11]int{{101, 5, 7, 6, 3, 2, 1, 0, 5, 4, 3}, {96, 0, 6, 7, 5, 3, 2, 1, 5, 4, 4}, {96, 7, 6, 2, 1, 4, 0, 5, 4, 3, 5}, {99, 3, 6, 2, 4, 5, 1, 0, 0, 7, 6}, {96, 0, 5, 6, 7, 3, 1, 2, 5, 4, 4}, {100, 2, 0, 3, 5, 4, 6, 7, 2, 1, 5}}
	o := orders[group]
	state := initial
	for i := 0; i < o[0]; i++ {
		offset := base + uint32(i)
		n1 := ((state[o[3]] ^ state[o[4]]) & state[o[1]]) ^ state[o[3]]
		n2 := bits.RotateLeft32(state[o[1]], 26) ^ bits.RotateLeft32(state[o[1]], 21) ^ bits.RotateLeft32(state[o[1]], 7)
		n4 := words[offset&127] + n1 + n2 + table[offset&63] + state[o[5]]
		n5 := bits.RotateLeft32(state[o[2]], 30) ^ bits.RotateLeft32(state[o[2]], 19) ^ bits.RotateLeft32(state[o[2]], 10)
		n6 := (state[o[2]] & state[o[6]]) | ((state[o[2]] | state[o[6]]) & state[o[7]])
		old := state[o[9]]
		state = [8]uint32{state[7], state[0], state[1], state[2], state[3], state[4], state[5], state[6]}
		state[o[10]] = n5 + n6 + n4
		state[o[8]] = old + n4
	}
	var result []byte
	for i := range state {
		result = binary.BigEndian.AppendUint32(result, state[i]+initial[i])
	}
	for i := 0; i < 16; i++ {
		result[i] ^= result[i+16]
	}
	result = result[:16]
	return binary.LittleEndian.AppendUint32(result, hongguoHashChecksum(result))
}

func hongguoHashRound(values [8]uint64, round uint64, group int) uint64 {
	indices := [8]int{int(values[0]>>56) * 8, int(values[1] >> 45 & 2040), int(values[2] >> 37 & 2040), int(values[3] >> 29 & 2040), int(values[4] >> 21 & 2040), int(values[5] >> 13 & 2040), int(values[6]>>8&255) * 8, int(values[7]&255) * 8}
	value := round
	for i, index := range indices {
		value ^= binary.LittleEndian.Uint64(hongguoSignTables[1600+group*16384+i*2048+index:])
	}
	return value
}

func hongguoHashSwap(src, xor [8]uint64, table [11]uint64, orders []byte, group int, typ *[8]uint64) [8]uint64 {
	var data [8]uint64
	hash := xor
	for i := range data {
		data[i] = src[orders[i]] ^ xor[i]
	}
	for round := 0; round < 10; round++ {
		var next [8]uint64
		for i := range next {
			var rotated [8]uint64
			for j := range rotated {
				rotated[j] = hash[(i+j)%8]
			}
			next[i] = hongguoHashRound(rotated, 0, group)
		}
		next[7] ^= table[round+1]
		previous := data
		for i := range data {
			var rotated [8]uint64
			for j := range rotated {
				rotated[j] = previous[(i+j)%8]
			}
			data[i] = hongguoHashRound(rotated, next[i], group)
		}
		hash = next
	}
	if typ == nil {
		for i := range src {
			src[i] ^= data[(8-i)%8] ^ xor[i]
		}
	} else {
		for i := 0; i < 6; i++ {
			src[i] = data[7-i] ^ typ[i+1]
		}
		src[6] = data[1] ^ typ[7] ^ src[7]
		src[7] = data[0] ^ typ[0]
	}
	return src
}

func hongguoHashBranchOne(group int, stamp uint32, query [32]byte, body [16]byte, ts []byte) []byte {
	seeds := [...]uint32{0x808a9c79, 0xf079807e, 0xbadf79c5, 0xa785d3ff, 0x82d8438c}
	count := seeds[group] + stamp
	initial := bits.RotateLeft64(uint64(stamp), -int(byte(count)))
	orders := []byte{5, 7, 1, 2, 4, 0, 6, 3, 0, 5, 2, 4, 1, 3, 7, 6, 5, 7, 2, 4, 1, 6, 3, 0, 3, 0, 2, 4, 6, 7, 1, 5, 4, 5, 0, 3, 6, 2, 1, 7}
	table := [11]uint64{0x87aeea5dab37cd6b, 0x7ff48becb4f54087, 0xb0724c06706bbd5d, 0x1fe5dfb1143e328d, 0x1a2331d00af4f1f2, 0xcaff7131bb1e71ba, 0x33385e1042752218, 0xff01ed65d4a441fb, 0x0adb1ec8828c80e8, 0x62475d12f4e06fe7, 0x00bd0b238da4fe72}
	for i := range table {
		table[i] = bits.RotateLeft64(table[i], -int((count+1)&63))
	}
	data := append(append(append([]byte(nil), query[:]...), body[:]...), ts...)
	data = append(data, 0x80)
	data = append(data, make([]byte, 11)...)
	var src, xor [8]uint64
	for i := range src {
		src[i] = binary.BigEndian.Uint64(data[i*8:])
		xor[i] = initial
	}
	swap := hongguoHashSwap(src, xor, table, orders[group*8:group*8+8], group, nil)
	src = [8]uint64{}
	src[7] = 416
	xor = [8]uint64{swap[4], swap[3], swap[2], swap[1], swap[0], swap[7], swap[6], swap[5]}
	resultWords := hongguoHashSwap(src, xor, table, orders[group*8:group*8+8], group, &swap)
	var result []byte
	for i := 7; i >= 0; i-- {
		word := binary.LittleEndian.AppendUint64(nil, resultWords[i])
		result = append(result, word[1], word[3], word[5], word[6])
	}
	for i := 0; i < 16; i++ {
		result[i] ^= result[i+16]
	}
	result = result[:16]
	return binary.LittleEndian.AppendUint32(result, hongguoHashChecksum(result))
}

func hongguoAESV3(data []byte, stamp uint32) ([]byte, error) {
	if len(data) < 248 || len(hongguoSignTables) != 83536 {
		return nil, errors.New("红果 Medusa 数据或固定表无效")
	}
	group := int(stamp & 3)
	sbox := hongguoSignTables[group*256 : group*256+256]
	seeds := [4]uint32{0xca025ddc, 0x823dc546, 0xc9420583, 0xc298225f}
	key := []byte{0xf1, 0x59, 0x33, 0x76, 0x76, 0x6e, 0xa9, 0x8d, 0x34, 0xf3, 0x1b, 0x05, 0x7a, 0x9d, 0x5b, 0xe4}
	iv := []byte{0x1f, 0xe1, 0x09, 0xa4, 0x12, 0x52, 0x83, 0xf4, 0x18, 0xde, 0x9e, 0x05, 0x1a, 0x96, 0x9e, 0x12}
	master := make([]byte, 48)
	for i := 0; i < 16; i++ {
		master[i] = byte(seeds[group]>>uint((i%4)*8)) ^ key[i]
	}
	rounds := uint(8)
	for i := 4; i < 12; i++ {
		idx := 4 * (i - 1)
		word := append([]byte(nil), master[idx:idx+4]...)
		if i&3 == 0 {
			word = []byte{byte(seeds[group]>>(rounds&24)) ^ sbox[word[1]], sbox[word[2]], sbox[word[3]], sbox[word[0]]}
		}
		rounds += 2
		for j := range word {
			master[idx+4+j] = word[j] ^ master[idx-12+j]
		}
	}
	con := [4][4]int{{1, 0, 2, 3}, {2, 0, 3, 1}, {0, 1, 3, 2}, {1, 0, 2, 3}}[group]
	order := [4][16]int{{0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3}, {0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11}, {0, 9, 14, 7, 4, 13, 2, 11, 8, 1, 6, 3, 12, 5, 10, 15}, {0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3}}[group]
	plain := make([]byte, 32)
	for i := 0; i < 31; i++ {
		b := data[i*8:]
		plain[i] = b[0]>>4&2 | b[1]&64 | b[2]>>2&1 | b[3]<<3&128 | b[4]>>1&4 | b[5]<<3&16 | b[6]<<5&32 | b[7]>>4&8
	}
	plain[31] = 1
	var encoded []byte
	for off := 0; off < 32; off += 16 {
		state := append([]byte(nil), plain[off:off+16]...)
		for i := range state {
			state[i] ^= iv[i]
		}
		add := func(offset int, permute bool) {
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					column := j
					if permute {
						column = con[j]
					}
					state[i*4+j] ^= master[offset+i*4+column]
				}
			}
		}
		add(0, true)
		for round := 1; round < 3; round++ {
			previous := append([]byte(nil), state...)
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					state[i*4+j] = sbox[previous[con[i]*4+j]]
				}
			}
			previous = append([]byte(nil), state...)
			for i := range state {
				state[i] = previous[order[i]]
			}
			if round == 1 {
				previous = append([]byte(nil), state...)
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						state[i*4+j] = previous[i*4+con[j]]
					}
				}
				for col := 0; col < 4; col++ {
					a := [4]byte{state[col], state[4+col], state[8+col], state[12+col]}
					sum := a[0] ^ a[1] ^ a[2] ^ a[3]
					for row := 0; row < 4; row++ {
						x := a[row] ^ a[(row+1)%4]
						twice := x << 1
						if x&128 != 0 {
							twice ^= 0x1b
						}
						state[row*4+col] = a[row] ^ sum ^ twice
					}
				}
			}
			add(round*16, true)
		}
		add(16, false)
		encoded = append(encoded, state...)
		iv = state
	}
	result := append([]byte(nil), data...)
	for i := 0; i < 31; i++ {
		k := encoded[i]
		b := result[i*8:]
		b[0] = b[0]&^32 | k<<4&32
		b[1] = b[1]&^64 | k&64
		b[2] = b[2]&^4 | k<<2&4
		b[3] = b[3]&^16 | k>>3&16
		b[4] = b[4]&^8 | k<<1&8
		b[5] = b[5]&^2 | k>>3&2
		b[6] = b[6]&^1 | k>>5&1
		b[7] = b[7]&127 | k<<4&128
	}
	return bytes.Join([][]byte{encoded[31:], result}, nil), nil
}
