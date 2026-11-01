package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/models"
)

// RewriteFunc 在选定候选后按上游模型名重写请求体（如 multipart 的 model 字段）。
// contentType 返回空表示沿用原值。
type RewriteFunc func(upstreamModel string) (body []byte, contentType string, err error)

// Select 在给定候选中按优先级/熔断/Key 可用性选出一个可尝试的组合。
// 返回的 Candidate 可能非空但 key 为空（渠道内无可用 Key），调用方可排除该渠道后重试。
func (r *Router) Select(cands []Candidate, displayModel string, excludedCh, failedKeys map[int64]bool) (*Candidate, *models.ChannelKey, string, error) {
	cand := r.pickByPriority(cands, excludedCh, displayModel)
	if cand == nil {
		return nil, nil, "", ErrChannelBusy
	}
	key, apiKey, err := r.pickKey(cand.Channel.ID, displayModel, failedKeys)
	if err != nil {
		return cand, nil, "", err
	}
	return cand, key, apiKey, nil
}

// ForwardRaw 原样转发非 JSON 请求体（multipart/二进制），复用候选选择、Key 池与故障转移。
// rewrite 可为 nil；否则在每次尝试前按上游模型名重写请求体。
func (r *Router) ForwardRaw(ctx context.Context, body []byte, displayModel, upstreamPath, method, contentType string, passHeaders map[string]string, penalizeChannels bool, rewrite RewriteFunc) (*Result, error) {
	cands, err := r.Candidates(ctx, displayModel)
	if err != nil {
		return nil, err
	}

	excludedCh := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	attempts := 0
	maxAttempts := 5

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("request cancelled: %w", err)
		}
		cand, key, apiKey, err := r.Select(cands, displayModel, excludedCh, failedKeys)
		if cand == nil {
			break
		}
		if err != nil {
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			excludedCh[cand.Channel.ID] = true
			continue
		}
		adpt := adapter.Get(cand.Channel.Adapter)
		if adpt == nil {
			lastErr = fmt.Errorf("channel %s: adapter %q not registered", cand.Channel.Name, cand.Channel.Adapter)
			excludedCh[cand.Channel.ID] = true
			continue
		}

		reqBody := body
		reqCT := contentType
		if rewrite != nil {
			rb, ct, rerr := rewrite(cand.Route.UpstreamModel)
			if rerr != nil {
				return nil, rerr
			}
			reqBody = rb
			if ct != "" {
				reqCT = ct
			}
		}

		resp, err := adpt.Do(ctx, adapter.ChannelFrom(&cand.Channel), apiKey, &adapter.Request{
			Method:      method,
			Path:        upstreamPath,
			Body:        bytes.NewReader(reqBody),
			ContentType: reqCT,
			Headers:     mergeHeaders(passHeaders, channelHeaders(&cand.Channel)),
			Model:       cand.Route.UpstreamModel,
		})
		if err != nil {
			attempts++
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			if penalizeChannels {
				r.recordChannelFail(&cand.Channel, 0)
			}
			excludedCh[cand.Channel.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		}
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamResp+1))
		resp.Body.Close()
		if err != nil {
			attempts++
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			if penalizeChannels {
				r.recordChannelFail(&cand.Channel, 0)
			}
			excludedCh[cand.Channel.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		}
		if len(respBody) > maxUpstreamResp {
			attempts++
			lastErr = fmt.Errorf("channel %s: upstream response too large", cand.Channel.Name)
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		}

		code := resp.StatusCode
		switch {
		case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired || code == http.StatusForbidden:
			attempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(respBody), 300))
			if code == http.StatusTooManyRequests {
				r.pen.PenalizeKey(key.ID, keyRatePenalty)
			} else {
				r.pen.PenalizeKey(key.ID, r.pen.QuotaPenalty())
			}
			failedKeys[key.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusUnauthorized:
			attempts++
			lastErr = fmt.Errorf("channel %s: unauthorized key", cand.Channel.Name)
			r.pen.PenalizeKey(key.ID, 5*time.Minute)
			failedKeys[key.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusRequestTimeout || code >= http.StatusInternalServerError:
			attempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(respBody), 300))
			if penalizeChannels {
				r.recordChannelFail(&cand.Channel, 0)
			}
			excludedCh[cand.Channel.ID] = true
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusBadRequest || code == http.StatusNotFound:
			attempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(respBody), 300))
			excludedCh[cand.Channel.ID] = true
			if penalizeChannels {
				r.recoverChannel(&cand.Channel)
			}
			if attempts >= maxAttempts {
				return nil, lastErr
			}
			continue
		default:
			r.recoverChannel(&cand.Channel)
			r.pen.ClearModelDenied(cand.Channel.ID, displayModel)
			r.pen.ClearModelDeniedKey(key.ID, displayModel)
			return &Result{
				StatusCode:  resp.StatusCode,
				Body:        respBody,
				ContentType: resp.Headers["Content-Type"],
				Channel:     cand.Channel,
				Route:       cand.Route,
				KeyID:       key.ID,
				KeyOwnerID:  key.UserID,
				Retries:     attempts,
			}, nil
		}
	}

	if len(cands) == 0 {
		return nil, ErrNoCandidate
	}
	if attempts > 0 {
		return nil, lastErr
	}
	return nil, ErrChannelBusy
}
