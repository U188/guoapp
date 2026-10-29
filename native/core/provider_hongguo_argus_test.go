package core

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

func TestHongguoArgusSignatureReferenceParity(t *testing.T) {
	query := "aid=8662&app_name=novelread&device_id=4263275890506393&iid=2715163518305450&version_code=73532&version_name=7.3.5.32"
	stub := "9f2b1c7a4d5e6f8091a2b3c4d5e6f708"
	signature, err := hongguoArgusSign(query, stub, 1793255000, hongguoSignAID, hongguoLicenseID, "4263275890506393", "", 12345)
	if err != nil {
		t.Fatal(err)
	}
	expected := "8oGtly/q30zomF9jsMsfO3JH1WF7TLiPqtHvN1FuiHsrhc+PdIrYefXosEK6ZZjO/HD+9JAyT6+bdDZ9vFPmRJocKXFRl9gQH18ldGzn5XS5UiNZhDm8aHUUQ1ZnYm0tLVL+4qVjWwo3/nCfcL0MQGRGyWG965PA1jSWbO01oarc+S/Y+GGd84d/454Cl/zoRdYIDIq1p+MUBmzbvCrBpteRLz4D8moHImk2qrPIjJjXl7nahVn0B/3+6aBby2aczRqtoVsNX1kSdj/1zJXPS+kk"
	if signature != expected {
		t.Fatalf("argus mismatch:\n got %s\nwant %s", signature, expected)
	}
}

func TestHongguoLadonSignatureReferenceParity(t *testing.T) {
	value := hongguoLadonEncrypt(1793255000, hongguoLicenseID, hongguoSignAID, []byte{0x11, 0x22, 0x33, 0x44})
	expected := "ESIzRA34N5NNHCmR/1DcEWvIFvliOFD9GEufakOjqE0b/iD0"
	if value != expected {
		t.Fatalf("ladon mismatch:\n got %s\nwant %s", value, expected)
	}
}

func TestHongguoPlatformHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://api5-normal-sinfonlineb.fqnovel.com/reading/distribution/category/landpage/v/?aid=8662&device_id=4263275890506393&iid=2715163518305450&version_name=7.3.5.32", bytes.NewReader([]byte(`{"req_scene":"default"}`)))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"req_scene":"default"}`)
	signHongguoPlatform(request, body, time.Unix(1793255000, 0))
	for _, name := range []string{"X-Argus", "X-Ladon"} {
		if request.Header.Get(name) == "" {
			t.Fatal("missing header", name)
		}
	}
}
