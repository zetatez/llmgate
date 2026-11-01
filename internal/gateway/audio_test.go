package gateway

import (
	"bytes"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testWAVHeader(byteRate, dataSize uint32) []byte {
	b := make([]byte, 44)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], 36+dataSize)
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:24], 1) // mono
	binary.LittleEndian.PutUint32(b[24:28], 16000)
	binary.LittleEndian.PutUint32(b[28:32], byteRate)
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], dataSize)
	return b
}

func multipartAudioBody(t *testing.T, model string, audio []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("model", model); err != nil {
		t.Fatal(err)
	}
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestWavDuration(t *testing.T) {
	if got := wavDuration(testWAVHeader(32000, 32000)); got != 1 {
		t.Fatalf("wavDuration = %v, want 1", got)
	}
	if got := wavDuration([]byte("not a wav")); got != 0 {
		t.Fatalf("non-wav duration = %v, want 0", got)
	}
}

func TestRewriteMultipartModel(t *testing.T) {
	body, ct := multipartAudioBody(t, "alias", testWAVHeader(32000, 100))
	out, outCT, err := rewriteMultipartModel(body.Bytes(), ct, "upstream-model")
	if err != nil {
		t.Fatal(err)
	}
	model, audio, _, _, err := multipartModelAndAudio(out, outCT)
	if err != nil {
		t.Fatal(err)
	}
	if model != "upstream-model" {
		t.Fatalf("model = %q, want upstream-model", model)
	}
	if len(audio) != 44 {
		t.Fatalf("audio bytes = %d, want 44", len(audio))
	}
}

func TestAudioTranscriptionForwardsMultipart(t *testing.T) {
	var gotModel string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		model, _, err := r.FormFile("file")
		_ = model
		if err == nil {
			// 读 multipart model 字段
			_ = r.ParseMultipartForm(1 << 20)
			gotModel = r.FormValue("model")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi","usage":{"seconds":2}}`))
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	body, ct := multipartAudioBody(t, "m", testWAVHeader(32000, 32000))
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	e.uh.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"hi"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotModel != "m" {
		t.Fatalf("upstream model = %q", gotModel)
	}
}

func TestAudioTranscriptionFallsBackToChatAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"native audio endpoint unsupported"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello world"}}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	body, ct := multipartAudioBody(t, "m", testWAVHeader(32000, 32000))
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	e.uh.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"text":"hello world"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAudioSpeechUnwrapsPCMJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"audio":"AAAAAA==","mime_type":"audio/pcm;rate=24000","usage":{"input_tokens":5,"output_tokens":0,"total_tokens":5}}`))
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/audio/speech", `{"model":"m","input":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/wav" {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "RIFF") {
		t.Fatalf("expected WAV body, got %x", rec.Body.Bytes()[:8])
	}
	if rec.Body.Len() != 48 {
		t.Fatalf("wav length = %d, want 48", rec.Body.Len())
	}
}

func TestAudioSpeechForwardsJsonAndAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"m"`) || !strings.Contains(string(body), `"input":"hello"`) {
			t.Errorf("unexpected upstream body: %s", body)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte{0xff, 0xfb, 0x90, 0x64})
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/audio/speech", `{"model":"m","input":"hello","voice":"alloy"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte{0xff, 0xfb, 0x90, 0x64}) {
		t.Fatalf("unexpected audio body: %x", rec.Body.Bytes())
	}
}
