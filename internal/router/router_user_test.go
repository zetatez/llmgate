package router

import (
	"context"
	"io"
	"net/http"
	"testing"

	"llmgate/internal/adapter"
	"llmgate/internal/store"
)

// TestSharedPoolUsesOtherUsersKey 验证共享池：请求可以使用任意用户的 Key，
// 并在结果中返回该 Key 的归属用户（贡献者）用于记账。
func TestSharedPoolUsesOtherUsersKey(t *testing.T) {
	b := newTB(t)
	runAdapter(t, &wireAdapter{
		name: "pool-ok",
		do: func(_ adapter.BaseChannel, key string, _ *adapter.Request) (*adapter.Response, error) {
			if key != "sk-a" {
				t.Fatalf("expect key sk-a, got %q", key)
			}
			return resp(200, `{"ok":true}`, "application/json"), nil
		},
		stream: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
			return nil, nil
		},
	})
	chID := b.ch("pool", "http://stub-pool", "pool-ok", 0, 1)
	b.route(chID, "m", "m", 0, 1)
	enc, err := b.mgr.Encrypt("sk-a")
	if err != nil {
		t.Fatal(err)
	}
	// Key 归属 user 7
	if err := store.AddChannelKeys(b.db, chID, []store.KeyInput{{UserID: 7, Encrypted: enc}}); err != nil {
		t.Fatal(err)
	}

	// user 8 没有自己的 Key，仍可使用池中 user 7 的 Key
	rt := b.router().SetUser(8)
	res, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil || res == nil || res.StatusCode != http.StatusOK {
		t.Fatalf("shared pool forward: res=%+v err=%v", res, err)
	}
	if res.KeyOwnerID != 7 {
		t.Fatalf("expected contributor 7, got %d", res.KeyOwnerID)
	}
}
