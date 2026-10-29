package route

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
)

// getConnectivity 返回各 provider 的连通性探测结果，用于排查「哪个源挂了」。
func getConnectivity(app *engine.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		results := app.CheckConnectivity(c.Request.Context())
		c.JSON(http.StatusOK, &responseMessage{Data: results})
	}
}
