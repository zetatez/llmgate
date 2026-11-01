package apiv1

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
)

// parseID 解析路径参数为 int64，失败返回 false（已写响应）。
func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return id, true
}

// keySecretView 是 Key 明文的可复制视图：约定 "AK:SK" 格式，无冒号时整体视为 SK。
type keySecretView struct {
	Key string `json:"key"`
	AK  string `json:"ak"`
	SK  string `json:"sk"`
}

func splitKeySecret(full string) keySecretView {
	full = strings.TrimSpace(full)
	if i := strings.IndexByte(full, ':'); i >= 0 {
		return keySecretView{Key: full, AK: strings.TrimSpace(full[:i]), SK: strings.TrimSpace(full[i+1:])}
	}
	return keySecretView{Key: full, SK: full}
}

// decryptKeySecret 解密渠道 Key 并返回可复制视图。
func decryptKeySecret(a *app.App, enc string) (keySecretView, error) {
	plain, err := a.Secret.Decrypt(enc)
	if err != nil {
		return keySecretView{}, err
	}
	return splitKeySecret(plain), nil
}

// decode 将请求体解析到 dst，失败返回 false（已写响应）。
func decode(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return false
	}
	return true
}
