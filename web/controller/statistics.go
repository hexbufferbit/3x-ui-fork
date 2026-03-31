package controller

import (
	"github.com/mhsanaei/3x-ui/v2/web/job"
	"github.com/mhsanaei/3x-ui/v2/web/service"

	"github.com/gin-gonic/gin"
)

type StatisticsController struct {
	BaseController
	statisticsService service.StatisticsService
}

func NewStatisticsController(g *gin.RouterGroup) *StatisticsController {
	a := &StatisticsController{}
	a.initRouter(g)
	return a
}

func (a *StatisticsController) initRouter(g *gin.RouterGroup) {
	g.GET("/stats", a.getStats)
	g.POST("/deleteStats", a.deleteStats)
}

func (a *StatisticsController) getStats(c *gin.Context) {
	onlineClients := job.GetLastOnlineClients()
	deltas := job.GetLastClientTrafficDeltas()
	stats, err := a.statisticsService.GetAllStats(onlineClients, deltas)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.getStatsFail"), err)
		return
	}
	jsonObj(c, stats, nil)
}

func (a *StatisticsController) deleteStats(c *gin.Context) {
	err := a.statisticsService.DeleteAllStats()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.deleteStatsFail"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.deleteStatsSuccess"), nil)
}
