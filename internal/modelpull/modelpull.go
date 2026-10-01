// Package modelpull 提供从上游渠道拉取模型并自动创建一一映射路由的能力。
// 手动按钮（apiv1）与定时任务（puller）共用此核心，保证幂等。
package modelpull

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// Options 拉取时新建路由的默认参数。
type Options struct {
	Priority int
	Weight   int
}

// Result 拉取（同步）结果。
type Result struct {
	Total   int      `json:"total"`
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Removed int      `json:"removed"` // 清理的不再存在的同名直通路由
	Models  []string `json:"models"`
}

// PullChannel 同步渠道的 /v1/models 与本地路由：
//   - 为缺失模型创建 display_name=上游模型名 的一一映射路由（幂等）；
//   - 删除上游已不再存在、且为"同名直通"的路由（手动别名路由保留）。
func PullChannel(ctx context.Context, a *app.App, ch *models.Channel, opt Options) (*Result, error) {
	apiKey, err := a.FirstEnabledKey(ch.ID)
	if err != nil {
		return nil, err
	}
	adpt := adapter.Get(ch.Adapter)
	if adpt == nil {
		return nil, fmt.Errorf("未知 adapter 类型: %s", ch.Adapter)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(ch.TimeoutMS)*time.Millisecond)
	defer cancel()

	resp, err := adpt.Do(ctx, adapter.ChannelFrom(ch), apiKey, adapter.TestRequest())
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		ct := resp.Headers["Content-Type"]
		if strings.Contains(strings.ToLower(ct), "html") {
			return nil, fmt.Errorf("上游返回 HTML 页面（HTTP %d），请检查 base_url 是否为 API 地址", resp.StatusCode)
		}
		return nil, fmt.Errorf("上游返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	names, err := adapter.ParseModels(body)
	if err != nil || len(names) == 0 {
		return nil, errors.New("上游未返回可用的模型列表")
	}

	res := &Result{Models: names}
	weight := opt.Weight
	if weight <= 0 {
		weight = 1
	}
	for _, m := range names {
		ns := NamespacedName(m)
		if ns == m {
			// 无法推断厂商前缀：保留裸名（否则该模型不可访问）
			exists, err := store.RouteExists(a.DB, ch.ID, m, m)
			if err != nil {
				return nil, err
			}
			if exists {
				res.Skipped++
			} else if _, err := store.CreateModelRoute(a.DB, &models.ModelRoute{
				DisplayName: m, ChannelID: ch.ID, UpstreamModel: m,
				Priority: opt.Priority, Weight: weight, Enabled: 1,
			}); err != nil {
				return nil, err
			} else {
				res.Created++
			}
			continue
		}

		// 统一厂商前缀命名：仅保留 vendor/model；删除对应的纯裸名自动路由
		existsNS, err := store.RouteExists(a.DB, ch.ID, ns, m)
		if err != nil {
			return nil, err
		}
		if existsNS {
			res.Skipped++
		} else if _, err := store.CreateModelRoute(a.DB, &models.ModelRoute{
			DisplayName: ns, ChannelID: ch.ID, UpstreamModel: m,
			Priority: opt.Priority, Weight: weight, Enabled: 1,
		}); err != nil {
			return nil, err
		} else {
			res.Created++
		}
		if _, err := store.DeleteAutoBare(a.DB, ch.ID, m); err != nil {
			return nil, err
		}
	}
	res.Total = len(names)

	// 清理上游已不存在的同名直通路由（确保路由信息最新、无陈旧条目）
	removed, err := store.RemoveStaleRoutes(a.DB, ch.ID, names)
	if err != nil {
		return nil, err
	}
	res.Removed = int(removed)
	return res, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
