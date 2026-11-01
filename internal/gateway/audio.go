package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

const (
	maxAudioBody = 128 << 20 // 128MB：足够覆盖常见录音文件
	// 音频计费换算：1 秒音频按 1000 tokens 计（可由模型单价换算为每分钟成本）。
	audioTokensPerSecond = 1000.0
	// TTS 输入按 4 字符 ≈ 1 token 估算。
	ttsCharsPerToken = 4.0
)

func readAudioBody(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxAudioBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxAudioBody {
		return nil, fmt.Errorf("request body too large")
	}
	return body, nil
}

func multipartBoundary(ct string) string {
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "multipart/form-data" {
		return ""
	}
	return params["boundary"]
}

// multipartModelAndAudio 读取 multipart 中的 model 字段与第一个文件（音频）内容。
func multipartModelAndAudio(body []byte, ct string) (model string, audio []byte, filename, audioCT string, err error) {
	boundary := multipartBoundary(ct)
	if boundary == "" {
		return "", nil, "", "", fmt.Errorf("expected multipart/form-data")
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			return "", nil, "", "", perr
		}
		if p.FormName() == "model" {
			b, _ := io.ReadAll(io.LimitReader(p, 4096))
			model = strings.TrimSpace(string(b))
			continue
		}
		if p.FileName() != "" && audio == nil {
			b, rerr := io.ReadAll(io.LimitReader(p, maxAudioBody))
			if rerr != nil {
				return "", nil, "", "", rerr
			}
			audio = b
			filename = p.FileName()
			audioCT = p.Header.Get("Content-Type")
		}
	}
	return model, audio, filename, audioCT, nil
}

// audioFormat 推断 input_audio.format（chat 回退用）。
func audioFormat(filename, contentType string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	switch ext {
	case "wav", "mp3", "m4a", "webm", "ogg", "flac":
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4", "audio/x-m4a":
		return "m4a"
	case "audio/webm":
		return "webm"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	}
	return "wav"
}

// transcriptionViaChat 把音频转成 chat/completions 的 input_audio 请求，
// 兼容只提供 chat 音频能力的模型（如 qwen3-asr-flash）。
func transcriptionViaChat(ctx context.Context, rt *router.Router, model string, audio []byte, format string, passHeaders map[string]string) (*router.Result, error) {
	dataURL := "data:audio/" + format + ";base64," + base64.StdEncoding.EncodeToString(audio)
	chatReq := map[string]any{
		"model": model,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type":        "input_audio",
				"input_audio": map[string]any{"data": dataURL, "format": format},
			}},
		}},
	}
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	res, err := rt.ForwardChat(ctx, body, model, "/v1/chat/completions", passHeaders)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, nil
	}
	text, usage, audioInfo := extractChatTranscript(res.Body)
	// 静音/纯音调可能返回空文本，仍是有效响应。
	out := map[string]any{"text": text}
	if usage != nil {
		out["usage"] = usage
	}
	if audioInfo != nil {
		out["audio_info"] = audioInfo
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	res.Body = b
	res.ContentType = "application/json"
	return res, nil
}

func extractChatTranscript(body []byte) (string, json.RawMessage, json.RawMessage) {
	var r struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage     json.RawMessage `json:"usage"`
		AudioInfo json.RawMessage `json:"audio_info"`
	}
	if err := json.Unmarshal(body, &r); err != nil || len(r.Choices) == 0 {
		return "", nil, nil
	}
	raw := r.Choices[0].Message.Content
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &parts); err == nil {
			var b strings.Builder
			for _, p := range parts {
				if p.Text != "" {
					b.WriteString(p.Text)
				}
			}
			text = b.String()
		}
	}
	return strings.TrimSpace(text), r.Usage, r.AudioInfo
}

// rewriteMultipartModel 重建 multipart 请求体，把 model 字段替换为上游模型名。
func rewriteMultipartModel(body []byte, ct, upstreamModel string) ([]byte, string, error) {
	boundary := multipartBoundary(ct)
	if boundary == "" {
		return nil, "", fmt.Errorf("expected multipart/form-data")
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		if p.FormName() == "model" {
			if err := w.WriteField("model", upstreamModel); err != nil {
				return nil, "", err
			}
			continue
		}
		dst, err := w.CreatePart(p.Header)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(dst, p); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "multipart/form-data; boundary=" + w.Boundary(), nil
}

// wavDuration 从 WAV 头计算时长（秒）；非 WAV 或解析失败返回 0。
func wavDuration(b []byte) float64 {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0
	}
	var byteRate uint32
	var dataSize uint32
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		dataOff := off + 8
		if id == "fmt " && dataOff+12 <= len(b) {
			byteRate = binary.LittleEndian.Uint32(b[dataOff+8 : dataOff+12])
		}
		if id == "data" {
			dataSize = uint32(size)
			break
		}
		off = dataOff + size + (size & 1)
	}
	if byteRate == 0 || dataSize == 0 {
		return 0
	}
	return float64(dataSize) / float64(byteRate)
}

// audioUsage 解析上游转写用量：优先 usage tokens，其次 duration/seconds。
func audioUsage(body []byte) (prompt, completion, total int64, duration float64) {
	var u struct {
		Usage struct {
			PromptTokens     int64   `json:"prompt_tokens"`
			CompletionTokens int64   `json:"completion_tokens"`
			TotalTokens      int64   `json:"total_tokens"`
			Seconds          float64 `json:"seconds"`
			Duration         float64 `json:"duration"`
		} `json:"usage"`
		Duration  float64 `json:"duration"`
		AudioInfo struct {
			Duration float64 `json:"duration"`
		} `json:"audio_info"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return 0, 0, 0, 0
	}
	if u.Usage.TotalTokens > 0 {
		return u.Usage.PromptTokens, u.Usage.CompletionTokens, u.Usage.TotalTokens, 0
	}
	duration = u.Usage.Seconds
	if duration == 0 {
		duration = u.Usage.Duration
	}
	if duration == 0 {
		duration = u.Duration
	}
	if duration == 0 {
		duration = u.AudioInfo.Duration
	}
	return 0, 0, 0, duration
}

func isAuthOrQuotaStatus(code int) bool {
	switch code {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	}
	return false
}

type speechJSON struct {
	Audio    string `json:"audio"`
	MimeType string `json:"mime_type"`
	Usage    struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

// unwrapSpeechResponse 处理上游把音频放在 JSON 里的情况；普通二进制响应返回 ok=false。
func unwrapSpeechResponse(body []byte, responseFormat string) (audio []byte, contentType string, prompt, completion, total int64, ok bool) {
	var r speechJSON
	if err := json.Unmarshal(body, &r); err != nil || r.Audio == "" {
		return nil, "", 0, 0, 0, false
	}
	data, err := base64.StdEncoding.DecodeString(r.Audio)
	if err != nil || len(data) == 0 {
		return nil, "", 0, 0, 0, false
	}
	contentType = r.MimeType
	if strings.HasPrefix(contentType, "audio/pcm") {
		rate := 24000
		if _, params, err := mime.ParseMediaType(contentType); err == nil {
			if v, err := strconv.Atoi(params["rate"]); err == nil && v > 0 {
				rate = v
			}
		}
		if strings.EqualFold(responseFormat, "pcm") {
			return data, contentType, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.TotalTokens, true
		}
		return pcmToWAV(data, rate), "audio/wav", r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.TotalTokens, true
	}
	if contentType == "" {
		contentType = detectAudioContentType(data)
	}
	return data, contentType, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.TotalTokens, true
}

// pcmToWAV 把 16-bit 单声道 PCM 包成 WAV；无 ffmpeg 转码时的通用兜底。
func pcmToWAV(pcm []byte, sampleRate int) []byte {
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+len(pcm)))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(len(pcm)))
	copy(buf[44:], pcm)
	return buf
}

func detectAudioContentType(data []byte) string {
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "audio/wav"
	}
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return "audio/mpeg"
	}
	if len(data) >= 2 && data[0] == 0xff && (data[1]&0xe0) == 0xe0 {
		return "audio/mpeg"
	}
	if len(data) >= 4 && string(data[0:4]) == "OggS" {
		return "audio/ogg"
	}
	return "audio/pcm"
}

// rewriteJSONModel 只替换 JSON 里的 model 字段，保留其余参数。
func rewriteJSONModel(body []byte, upstreamModel string) ([]byte, string, error) {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, "", err
	}
	v["model"] = upstreamModel
	b, err := json.Marshal(v)
	return b, "application/json", err
}

// handleAudioTranscription 处理 POST /v1/audio/transcriptions（multipart）。
func handleAudioTranscription(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := readAudioBody(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			return
		}
		ct := c.GetHeader("Content-Type")
		model, audio, filename, audioCT, err := multipartModelAndAudio(raw, ct)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			return
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing model field", "type": "invalid_request_error"}})
			return
		}

		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen).SetUser(user.ID)
		passHeaders := buildPassHeaders(c.Request.Header)

		logEntry := &models.RequestLog{
			UserID:       user.ID,
			DisplayModel: model,
			CreatedAt:    models.Now(),
		}
		start := time.Now()
		res, ferr := rt.ForwardRaw(c.Request.Context(), raw, model, "/v1/audio/transcriptions", http.MethodPost, ct, passHeaders, false,
			func(upstreamModel string) ([]byte, string, error) {
				return rewriteMultipartModel(raw, ct, upstreamModel)
			})
		// 部分上游只支持 chat/completions 的 input_audio（如 qwen3-asr-flash），
		// 原生 /audio/transcriptions 不可用时回退为 chat 音频请求。
		if len(audio) > 0 && (ferr != nil || (res != nil && res.StatusCode >= 400 && !isAuthOrQuotaStatus(res.StatusCode))) {
			if fbRes, fbErr := transcriptionViaChat(c.Request.Context(), rt, model, audio, audioFormat(filename, audioCT), passHeaders); fbErr == nil && fbRes != nil && fbRes.StatusCode >= 200 && fbRes.StatusCode < 300 {
				res, ferr = fbRes, nil
			}
		}
		logEntry.LatencyMS = time.Since(start).Milliseconds()

		if res != nil {
			logEntry.ChannelID = res.Channel.ID
			logEntry.KeyID = res.KeyID
			logEntry.KeyOwnerID = res.KeyOwnerID
			logEntry.UpstreamModel = res.Route.UpstreamModel
			logEntry.Status = successOrError(res.StatusCode)
			if logEntry.Status != "success" {
				logEntry.ErrorCode = oneLine(string(res.Body), 200)
			} else {
				prompt, completion, total, duration := audioUsage(res.Body)
				if total == 0 {
					if duration == 0 {
						duration = wavDuration(audio)
					}
					if duration > 0 {
						prompt = int64(math.Ceil(duration * audioTokensPerSecond))
						total = prompt
					}
				}
				logEntry.PromptTokens, logEntry.CompletionTokens, logEntry.TotalTokens = prompt, completion, total
			}
			finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
			if logEntry.Status == "success" {
				_ = store.ApplyUsage(a.DB, logEntry.UserID, logEntry.KeyOwnerID, logEntry.PromptTokens, logEntry.CompletionTokens, logEntry.Cost)
			}
			resCT := res.ContentType
			if resCT == "" {
				resCT = "application/json"
			}
			c.Data(res.StatusCode, resCT, res.Body)
			return
		}

		logEntry.Status = "error"
		logEntry.ErrorCode = oneLine(ferr.Error(), 200)
		finalizeLog(a, bus, logEntry, nil, nil)
		code, msg := mapRouteError(ferr, model)
		if retryAfter := retryAfterFor(code); retryAfter != "" {
			c.Header("Retry-After", retryAfter)
		}
		c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
	}
}

// handleAudioSpeech 处理 POST /v1/audio/speech（JSON，返回二进制音频）。
func handleAudioSpeech(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := readAudioBody(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			return
		}
		var meta struct {
			Model          string `json:"model"`
			Input          string `json:"input"`
			ResponseFormat string `json:"response_format"`
		}
		if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing valid model field", "type": "invalid_request_error"}})
			return
		}

		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen).SetUser(user.ID)
		passHeaders := buildPassHeaders(c.Request.Header)

		logEntry := &models.RequestLog{
			UserID:       user.ID,
			DisplayModel: meta.Model,
			CreatedAt:    models.Now(),
		}
		start := time.Now()
		res, ferr := rt.ForwardRaw(c.Request.Context(), body, meta.Model, "/v1/audio/speech", http.MethodPost, "application/json", passHeaders, false,
			func(upstreamModel string) ([]byte, string, error) {
				return rewriteJSONModel(body, upstreamModel)
			})
		logEntry.LatencyMS = time.Since(start).Milliseconds()

		if res != nil {
			logEntry.ChannelID = res.Channel.ID
			logEntry.KeyID = res.KeyID
			logEntry.KeyOwnerID = res.KeyOwnerID
			logEntry.UpstreamModel = res.Route.UpstreamModel
			logEntry.Status = successOrError(res.StatusCode)
			if logEntry.Status == "success" {
				// 部分上游把音频包在 JSON 里（qwen/grok 等），解包为真实音频字节。
				if audio, ct, p, cp, tt, ok := unwrapSpeechResponse(res.Body, meta.ResponseFormat); ok {
					res.Body = audio
					res.ContentType = ct
					if tt > 0 {
						logEntry.PromptTokens, logEntry.CompletionTokens, logEntry.TotalTokens = p, cp, tt
					}
				}
				if logEntry.TotalTokens == 0 {
					// 没有 usage 时按输入字符数估算，便于统一走模型单价。
					tokens := int64(math.Ceil(float64(len([]rune(meta.Input))) / ttsCharsPerToken))
					logEntry.PromptTokens, logEntry.TotalTokens = tokens, tokens
				}
			} else {
				logEntry.ErrorCode = oneLine(string(res.Body), 200)
			}
			finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
			if logEntry.Status == "success" {
				_ = store.ApplyUsage(a.DB, logEntry.UserID, logEntry.KeyOwnerID, logEntry.PromptTokens, logEntry.CompletionTokens, logEntry.Cost)
			}
			resCT := res.ContentType
			if resCT == "" {
				resCT = "audio/mpeg"
			}
			c.Data(res.StatusCode, resCT, res.Body)
			return
		}

		logEntry.Status = "error"
		logEntry.ErrorCode = oneLine(ferr.Error(), 200)
		finalizeLog(a, bus, logEntry, nil, nil)
		code, msg := mapRouteError(ferr, meta.Model)
		if retryAfter := retryAfterFor(code); retryAfter != "" {
			c.Header("Retry-After", retryAfter)
		}
		c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
	}
}
