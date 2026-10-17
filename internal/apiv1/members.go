package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/store"
)

// listMembers 返回成员公开数据（用户名 / 注册时间 / 消费与贡献），
// 管理员与普通用户共用同一份只读数据。
func listMembers(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := store.ListMemberStats(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}
