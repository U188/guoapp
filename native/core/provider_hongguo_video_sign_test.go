package core

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestHongguoVideoSignatureReferenceParity(t *testing.T) {
	data, err := os.ReadFile("testdata/hongguo_video_sign_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Stamp                                     uint32
		Query, Body, Hash, Gorgon, Helios, Medusa string
		Branch                                    int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	branches := map[int]bool{}
	for _, row := range rows {
		branches[row.Branch] = true
		now := time.Unix(int64(row.Stamp), 0)
		query := hongguoVideoQuery("2345678901234567890", "1234567890123456789", now, hongguoVideoIdentityLegacy)
		if query != row.Query {
			t.Fatalf("query differs at %d", row.Stamp)
		}
		body, err := hongguoVideoPayload("7696746275915451454")
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != row.Body {
			t.Fatal("payload differs")
		}
		bh := md5.Sum(body)
		qh := hongguoSM3([]byte(query))
		hash, err := hongguoSignatureHash(qh, bh, row.Stamp)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(hash) != row.Hash {
			t.Fatalf("hash branch %d stamp %d: got %x want %s", row.Branch, row.Stamp, hash, row.Hash)
		}
		if value := hongguoVideoGorgon(query, bh, row.Stamp, 0x1234); value != row.Gorgon {
			t.Fatalf("gorgon differs: %s != %s", value, row.Gorgon)
		}
		if value := hongguoVideoHelios(row.Stamp, 0x98765432); value != row.Helios {
			t.Fatal("helios differs")
		}
		nonce := hongguoVideoNonce{gorgon: 0x1234, helios: 0x98765432, proto: 0xf2345678, hash: 0x12345678, xm: 0xa2345678, launch: 110, pid: 11234}
		medusa, err := hongguoMedusa(query, bh, "2345678901234567890", row.Stamp, nonce)
		if err != nil {
			t.Fatal(err)
		}
		if medusa != row.Medusa {
			t.Fatalf("medusa differs at %d", row.Stamp)
		}
	}
	if len(branches) != 3 {
		t.Fatal("all hash branches must be covered")
	}
}

func TestHongguoMultiVideoRequestAndIdentity(t *testing.T) {
	d := sourceFixtureDownloader(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != hongguoMultiVideoPath {
			t.Fatal("wrong playback route", req.URL)
		}
		for _, name := range []string{"X-Argus", "X-Ladon", "X-SS-STUB", "X-Khronos"} {
			if req.Header.Get(name) == "" {
				t.Fatal("missing signature", name)
			}
		}
		body, _ := io.ReadAll(req.Body)
		expected, _ := hongguoVideoPayload("123")
		if !bytes.Equal(body, expected) {
			t.Fatal("wrong actual request bytes")
		}
		return sourceFixtureResponse(req, 200, `{"code":0,"data":{"123":{"video_model":"{\"video_list\":[{\"main_url\":\"https://media.test/play.mp4\",\"definition\":\"720p\"}]}"}}}`), nil
	})
	result, err := d.hongguoVideoModelRequest(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	model, err := hongguoVideoModel(result, "123")
	if err != nil {
		t.Fatal(err)
	}
	media, err := selectHongguoAppMedia(model)
	if err != nil || media.Quality != 720 {
		t.Fatal(media, err)
	}
	if _, err := hongguoVideoModel(result, "456"); err == nil {
		t.Fatal("accepted a different episode")
	}
}

func TestHongguoVideoSigningTableIntegrity(t *testing.T) {
	if len(hongguoSignTables) != 83536 {
		t.Fatal("wrong fixed table length")
	}
	for _, stamp := range []uint32{1, 2, 3, 4} {
		data := make([]byte, 512)
		binary.LittleEndian.PutUint32(data, stamp)
		if _, err := hongguoAESV3(data, stamp); err != nil {
			t.Fatal(err)
		}
	}
}
