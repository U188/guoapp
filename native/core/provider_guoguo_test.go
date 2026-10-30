package core

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func gzipBytes(t *testing.T, input []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(input); err != nil {
		t.Fatalf("写入 gzip 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败: %v", err)
	}
	return buf.Bytes()
}

func TestGuoguoDecodeBodyUnwrapsGzip(t *testing.T) {
	payload := []byte(`{"code":0,"msg":"success","data":{"token":"abc"}}`)
	plain, err := guoguoDecodeBody(gzipBytes(t, payload))
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !bytes.Equal(plain, payload) {
		t.Fatalf("解压结果不一致: %s", plain)
	}
}

func TestGuoguoDecodeBodyKeepsPlainJSON(t *testing.T) {
	payload := []byte(`"ZW5jcnlwdGVk"`)
	plain, err := guoguoDecodeBody(payload)
	if err != nil {
		t.Fatalf("明文解析失败: %v", err)
	}
	if !bytes.Equal(plain, payload) {
		t.Fatalf("明文被改写: %s", plain)
	}
}

func TestGuoguoParseReadsDecryptedLogin(t *testing.T) {
	// 登录接口返回的是 base64 密文，这里只验证 gzip 之后再解析不会丢掉字段。
	body := gzipBytes(t, []byte(`{"code":0,"msg":"success"}`))
	plain, err := guoguoDecodeBody(body)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	parsed, err := guoguoParse(plain)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if mapString(parsed, "code") != "0" {
		t.Fatalf("code 异常: %v", parsed["code"])
	}
}
