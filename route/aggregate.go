package route

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/metatube-community/metatube-sdk-go/engine"
)

// aggregateQuery 是 /v1/movies/aggregate 的查询参数。field_priority 为
// 可选的 JSON 字符串，形如 {"Title":["JavBus","FANZA"],"Summary":["FANZA"]}，
// 键为 model.MovieInfo 的 Go 字段名。
type aggregateQuery struct {
	Q             string `form:"q" binding:"required"`
	FieldPriority string `form:"field_priority"`
}

// getAggregateMovie 跨源按字段聚合元数据：身份字段取最高优先级源，
// 其余字段按 field_priority（未指定时按搜索优先级）逐字段取优。
func getAggregateMovie(app *engine.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := &aggregateQuery{}
		if err := c.ShouldBindQuery(query); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, err)
			return
		}

		var fieldPriority engine.FieldPriority
		if query.FieldPriority != "" {
			if err := json.Unmarshal([]byte(query.FieldPriority), &fieldPriority); err != nil {
				abortWithStatusMessage(c, http.StatusBadRequest, err)
				return
			}
		}

		info, err := app.AggregateMovie(query.Q, fieldPriority)
		if err != nil {
			abortWithError(c, err)
			return
		}

		c.JSON(http.StatusOK, &responseMessage{Data: info})
	}
}
