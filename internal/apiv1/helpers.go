package apiv1

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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

// decode 将请求体解析到 dst，失败返回 false（已写响应）。
func decode(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return false
	}
	return true
}
