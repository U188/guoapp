package core

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func dsdTestAaChar(octal string) string {
	parts := make([]string, 0, len(octal))
	for _, digit := range octal {
		switch digit {
		case '0':
			parts = append(parts, "(c^_^o)")
		case '1':
			parts = append(parts, "(ﾟΘﾟ)")
		case '3':
			parts = append(parts, "(o^_^o)")
		case '4':
			parts = append(parts, "(ﾟｰﾟ)")
		default:
			parts = append(parts, string(digit))
		}
	}
	return "(ﾟДﾟ)[ﾟεﾟ]+" + strings.Join(parts, "+")
}

func TestDSDAaencodeDecoderExtractsSignedVPath(t *testing.T) {
	payload := `var vPath = "/video/fixture/1000k/index.m3u8?sign=abc%2Fdef";`
	encoded := "(ﾟДﾟ) ['_'] ( (ﾟДﾟ) ['_']"
	for _, char := range payload {
		encoded += dsdTestAaChar(strconv.FormatInt(int64(char), 8))
	}
	encoded += "+ (ﾟДﾟ)[ﾟoﾟ]"
	page := `<html><script type="text/javascript">` + encoded + `</script></html>`
	path, ok := dsdSignedPathFromVplayer(page)
	if !ok || path != "/video/fixture/1000k/index.m3u8?sign=abc%2Fdef" {
		t.Fatalf("signed vPath not decoded: %q %v", path, ok)
	}
}

func TestDSDVplayerMediaPathDropsOrigin(t *testing.T) {
	got := dsdVplayerMediaPath("https://www.dsd.com.se/video/a/1000k/index.m3u8?token=a%2Fb")
	if got != "/video/a/1000k/index.m3u8?token=a%2Fb" {
		t.Fatalf("wrong media path: %s", got)
	}
	if dsdVplayerMediaPath("/video/a/index.m3u8") != "/video/a/index.m3u8" {
		t.Fatal("relative media path changed")
	}
}

func TestDSDPlayerIgnoresNonJSONAssignments(t *testing.T) {
	document, err := html.Parse(strings.NewReader(`<html><body><script>
var player_config = null;
var player_placeholder = true;
var player_aaaa = {"id":"100","url":"https://media.test/a.m3u8","sid":1,"nid":2};
</script></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	player, found, err := parseDSDPlayer(document)
	if err != nil {
		t.Fatalf("非 JSON 赋值不应中断解析: %v", err)
	}
	if !found || player == nil {
		t.Fatal("没有找到播放器参数")
	}
	if mapString(player, "url") != "https://media.test/a.m3u8" {
		t.Fatalf("播放器地址异常: %v", player["url"])
	}
}
