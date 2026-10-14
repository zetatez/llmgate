package router

import (
	"context"
	"errors"
	"io"
	"testing"

	"llmgate/internal/adapter"
	"llmgate/internal/store"
)

// TestPickKeyScopedToUser 验证多租户 BYOK：网关请求只使用请求方自己录入的 Key。
func TestPickKeyScopedToUser(t *testing.T) {
	b := newTB(t)
	runAdapter(t, &wireAdapter{
		name: "us-ok",
		do: func(_ adapter.BaseChannel, key string, _ *adapter.Request) (*adapter.Response, error) {
			if key != "sk-a" {
				t.Fatalf("expect user key sk-a, got %q", key)
			}
			return resp(200, `{"ok":true}`, "application/json"), nil
		},
		stream: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
			return nil, nil
		},
	})
	chID := b.ch("u", "http://stub-u", "us-ok", 0, 1)
	b.route(chID, "m", "m", 0, 1)

	// 渠道下只有 user 7 的 Key（user_id=7）
	enc, err := b.mgr.Encrypt("sk-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddChannelKeys(b.db, chID, []store.KeyInput{{UserID: 7, Encrypted: enc}}); err != nil {
		t.Fatal(err)
	}

	// user 7 → 走自己的 Key，成功
	rtA := b.router().SetUser(7)
	res, err := rtA.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil || res == nil || res.KeyID == 0 {
		t.Fatalf("user 7 should route via own key: res=%+v err=%v", res, err)
	}

	// user 8 没有任何 Key → 该模型对其无候选（404）
	rtB := b.router().SetUser(8)
	if _, err := rtB.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil); !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("user 8 without key should get ErrNoCandidate, got %v", err)
	}

	// 管理员/全局（user 0）也不得使用用户私有 Key
	rtAdmin := b.router().SetUser(0)
	if _, err := rtAdmin.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil); !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("admin scope should not use user keys, got %v", err)
	}
}
