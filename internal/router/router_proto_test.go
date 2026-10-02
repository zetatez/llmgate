package router

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"llmgate/internal/adapter"
	"llmgate/internal/models"
	"llmgate/internal/secret"
	"llmgate/internal/store"
)

// fakeProtoAdapter 永远返回 ModelProtocolUnsupported 的 400，用于验证确定性透传。
type fakeProtoAdapter struct{}

const protoBodyFixture = `{"type":"error","error":{"type":"ModelProtocolUnsupported","message":"Model does not support this protocol."}}`

func (fakeProtoAdapter) Name() string { return "fakeproto" }

// 非流式 Do 契约：4xx 也返回非 nil Response（StatusCode 非 2xx），err 为 nil。
func (fakeProtoAdapter) Do(_ context.Context, _ adapter.BaseChannel, _ string, _ *adapter.Request) (*adapter.Response, error) {
	return &adapter.Response{
		StatusCode: 400,
		Body:       io.NopCloser(strings.NewReader(protoBodyFixture)),
		Headers:    map[string]string{"Content-Type": "application/json"},
	}, nil
}
func (fakeProtoAdapter) DoStream(_ context.Context, _ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}

// TestForwardChatProtocolUnsupported: 上游 400(ModelProtocolUnsupported) 应以 400 透传返回，
// 并标记该模型在此渠道拒权（快速跳过），而不是当作临时故障重试/回 5xx。
func TestForwardChatProtocolUnsupported(t *testing.T) {
	adapter.Register(fakeProtoAdapter{})

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	chID, err := store.CreateChannel(db, &models.Channel{
		Name: "proto", BaseURL: "http://x", Adapter: "fakeproto",
		Priority: 0, Weight: 1, TimeoutMS: 3000, Enabled: 1, HealthState: "healthy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateModelRoute(db, &models.ModelRoute{
		DisplayName: "m", ChannelID: chID, UpstreamModel: "m", Priority: 0, Weight: 1, Enabled: 1,
	}); err != nil {
		t.Fatal(err)
	}

	mgr, _, err := secret.New("aabbccddeeff00112233445566778899") // 32 字节 hex
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := mgr.Encrypt("sk-test")
	if err := store.AddChannelKeys(db, chID, []store.KeyInput{{Encrypted: enc}}); err != nil {
		t.Fatal(err)
	}

	r := &Router{db: db, secret: mgr, pen: NewPenalizer()}
	body := []byte(`{"model":"m","stream":false,"messages":[{"role":"user","content":"hi"}]}`)

	res, ferr := r.ForwardChat(context.Background(), body, "m", "/v1/chat/completions", nil)
	if ferr != nil {
		t.Fatalf("expected passthrough result, got err: %v", ferr)
	}
	if res == nil || res.StatusCode != 400 {
		t.Fatalf("expected 400 passthrough, got %+v", res)
	}
	if !strings.Contains(string(res.Body), "ModelProtocolUnsupported") {
		t.Fatalf("body should contain upstream message, got %s", res.Body)
	}
	if !r.pen.ModelDenied(chID, "m") {
		t.Fatal("model should be marked denied on this channel (fail fast next time)")
	}
}
