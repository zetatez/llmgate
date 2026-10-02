package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	before := time.Now().Unix()
	got := Now()
	after := time.Now().Unix()
	if got < before || got > after {
		t.Fatalf("Now() = %d not within [%d,%d]", got, before, after)
	}
}

// 结构体 JSON 序列化应正确（可空价格指针、隐藏敏感字段）。
func TestJSONTags(t *testing.T) {
	pi := 0.5
	r := ModelRoute{
		ID: 1, DisplayName: "m", ChannelID: 2, UpstreamModel: "up",
		Priority: 0, Weight: 1, Enabled: 1, PriceInput: &pi,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back ModelRoute
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != 1 || back.DisplayName != "m" || back.PriceInput == nil || *back.PriceInput != 0.5 {
		t.Fatalf("route json roundtrip mismatch: %+v", back)
	}

	u := User{ID: 3, TokenHash: "h", TokenEnc: "e"}
	ub, _ := json.Marshal(u)
	var seen map[string]any
	_ = json.Unmarshal(ub, &seen)
	if _, ok := seen["token_hash"]; ok {
		t.Fatalf("token_hash must not be serialized: %s", ub)
	}
	if _, ok := seen["token_enc"]; ok {
		t.Fatalf("token_enc must not be serialized: %s", ub)
	}
}
