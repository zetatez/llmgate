package gateway

import (
	"database/sql"
	"strconv"
	"sync"

	"llmgate/internal/models"
	"llmgate/internal/store"
)

// touchInterval 同一 Key/令牌的 last_used_at 最短写库间隔（秒）。
// 网关每个请求都写 last_used_at 意义不大，却会在单连接 SQLite 上放大写竞争；
// 节流后在内存记录最近写入时间，间隔内不再落库。
const touchInterval = 60

var (
	touchMu   sync.Mutex
	touchSeen = map[string]int64{}
)

// shouldTouch 判断某个键（kind:id）是否已过节流窗口，需要写库。
func shouldTouch(kind string, id int64) bool {
	if id <= 0 {
		return false
	}
	now := models.Now()
	k := kind + ":" + strconv.FormatInt(id, 10)
	touchMu.Lock()
	defer touchMu.Unlock()
	if last, ok := touchSeen[k]; ok && now-last < touchInterval {
		return false
	}
	if len(touchSeen) > 100000 { // 防无界增长：达到上限时整体重置
		touchSeen = map[string]int64{}
	}
	touchSeen[k] = now
	return true
}

// touchChannelKey 节流地更新渠道 Key 最近使用时间。
func touchChannelKey(db *sql.DB, id int64) {
	if shouldTouch("ck", id) {
		_ = store.TouchChannelKey(db, id)
	}
}

// touchUserToken 节流地更新消费令牌最近使用时间。
func touchUserToken(db *sql.DB, id int64) {
	if shouldTouch("ut", id) {
		_ = store.TouchUserToken(db, id)
	}
}
