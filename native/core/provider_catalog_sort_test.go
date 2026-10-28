package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestCatalogSortChoicesMatchValidation(t *testing.T) {
	for _, source := range []string{sourceHuangju, sourceXChina} {
		choices := catalogSortChoices(source)
		if len(choices) == 0 {
			t.Fatalf("%s 没有排序选项", source)
		}
		for _, choice := range choices {
			id, _ := choice["id"].(string)
			name, _ := choice["name"].(string)
			if id == "" || name == "" {
				t.Fatalf("%s 排序选项缺字段: %+v", source, choice)
			}
			if !validCatalogSort(source, id) {
				t.Fatalf("%s 下发的排序值 %q 未通过校验", source, id)
			}
		}
	}
	if got := catalogSortChoices(sourceHongguo); got != nil {
		t.Fatalf("不支持的站源不应返回排序选项: %+v", got)
	}
	if validCatalogSort(sourceXChina, "new") {
		t.Fatal("sort=new 不属于 XChina 选项")
	}
}

// TestHuangjuNewestChannelUsesNewSort 锁定「最新」频道必须走 sort=new。
// 站点 new=纯时间倒序（实测 id 连续递减 3834→3827），latest 是混合推荐排序，
// hot 是最热；三者内容完全不同，判断依据必须是 id 单调性而不是首条标题。
func TestHuangjuNewestChannelUsesNewSort(t *testing.T) {
	var got string
	d := providerParityDownloader(t, func(request *http.Request, form url.Values) (*http.Response, error) {
		if request.URL.Path == "/auth/guest" {
			return sourceFixtureResponse(request, http.StatusOK, `{"token":"guest-token"}`), nil
		}
		got = request.URL.Query().Get("sort")
		return sourceFixtureResponse(request, http.StatusOK, `{"items":[],"page":1,"pageSize":20,"total":0}`), nil
	})
	if _, _, err := d.fetchHuangjuCatalogPage(context.Background(), 1, huangjuNewestCategory, ""); err != nil {
		t.Fatal(err)
	}
	if got != "new" {
		t.Fatalf("最新频道排序应为 new，实际 %q", got)
	}
}

func TestXChinaRetryableClassification(t *testing.T) {
	if !xchinaRetryable(xchinaBlockedError{}) {
		t.Fatal("限流错误必须可重试")
	}
	if !xchinaRetryable(xchinaTransientError{errors.New("timeout")}) {
		t.Fatal("传输层错误必须可重试")
	}
	if xchinaRetryable(errors.New("bad payload")) {
		t.Fatal("普通错误不应重试")
	}
	var target xchinaTransientError
	if err := error(xchinaTransientError{errors.New("inner")}); !errors.As(err, &target) {
		t.Fatal("transient 错误应可被 errors.As 识别")
	}
	if target.Unwrap().Error() != "inner" {
		t.Fatal("transient 错误丢失内部错误")
	}
}

func TestXChinaPageSizeMatchesScript(t *testing.T) {
	if xchinaPageSize != 24 {
		t.Fatalf("分页大小应与参考脚本 limit 一致: %d", xchinaPageSize)
	}
}
