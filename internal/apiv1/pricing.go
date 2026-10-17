// Package apiv1：全局模型价格的读取与维护接口。
package apiv1

import (
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterPricingRoutes 挂载管理员价格维护接口。
func RegisterPricingRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", listPricing(a))
	g.PUT("", upsertPricing(a))
	g.POST("/delete", deletePricing(a))
}

// listPricing 价格公示（管理员与普通用户共用，只读）。
func listPricing(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListModelPrices(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	}
}

// upsertPricing 批量新增/更新价格（管理员）。
func upsertPricing(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Data []models.ModelPrice `json:"data"`
		}
		if !decode(c, &b) {
			return
		}
		if len(b.Data) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "data is required"})
			return
		}
		if len(b.Data) > 5000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "too many rows (max 5000)"})
			return
		}
		for i := range b.Data {
			p := &b.Data[i]
			p.Model = strings.TrimSpace(p.Model)
			if p.Model == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
				return
			}
			if invalidPrice(p.Input) || invalidPrice(p.Output) || invalidPrice(p.Call) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "prices must be finite and >= 0"})
				return
			}
		}
		if err := store.UpsertModelPrices(a.DB, b.Data); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		rows, err := store.ListModelPrices(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	}
}

// deletePricing 批量删除价格（管理员）。
func deletePricing(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Models []string `json:"models"`
		}
		if !decode(c, &b) {
			return
		}
		if len(b.Models) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "models is required"})
			return
		}
		if err := store.DeleteModelPrices(a.DB, b.Models); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		rows, err := store.ListModelPrices(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	}
}

func invalidPrice(v float64) bool {
	return v < 0 || math.IsNaN(v) || math.IsInf(v, 0)
}
