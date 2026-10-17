package apiv1

import (
	"net/http"
	"testing"
)

// TestModelPricingAdminAndUser 校验价格表的批量维护、删除，以及普通用户只读。
func TestModelPricingAdminAndUser(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	cookie := userLoginCookie(t, r, "alice", "secret1")

	// 管理员批量写入
	body := `{"data":[{"model":"deepseek-chat","input":0.14,"output":0.28},{"model":"gpt-x","input":2.5,"output":10,"call":0.01}]}`
	w := do(r, http.MethodPut, basePath+"/pricing", body, testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("upsert pricing = %d, body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Data []struct {
			Model  string  `json:"model"`
			Input  float64 `json:"input"`
			Output float64 `json:"output"`
			Call   float64 `json:"call"`
		} `json:"data"`
	}
	decodeBody(t, w, &list)
	if len(list.Data) != 2 || list.Data[0].Model != "deepseek-chat" || list.Data[1].Call != 0.01 {
		t.Fatalf("pricing list = %+v", list.Data)
	}

	// 普通用户可读
	w = doCookie(r, http.MethodGet, "/api/user/pricing", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("user read pricing = %d, body=%s", w.Code, w.Body.String())
	}
	decodeBody(t, w, &list)
	if len(list.Data) != 2 {
		t.Fatalf("user pricing list = %+v", list.Data)
	}

	// 普通用户不可写
	if w := doCookie(r, http.MethodPut, "/api/user/pricing", body, cookie); w.Code == http.StatusOK {
		t.Fatal("user should not be able to update pricing")
	}

	// 非法价格
	if w := do(r, http.MethodPut, basePath+"/pricing", `{"data":[{"model":"x","input":-1}]}`, testAdminToken, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("negative price = %d, want 400", w.Code)
	}

	// 批量删除
	w = do(r, http.MethodPost, basePath+"/pricing/delete", `{"models":["gpt-x"]}`, testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete pricing = %d, body=%s", w.Code, w.Body.String())
	}
	decodeBody(t, w, &list)
	if len(list.Data) != 1 || list.Data[0].Model != "deepseek-chat" {
		t.Fatalf("pricing after delete = %+v", list.Data)
	}
}
