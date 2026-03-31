package controller

import (
	"strings"
	"time"

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
	g.GET("/ipInfo", a.getIPInfo)
	g.POST("/deleteStats", a.deleteStats)
}

func (a *StatisticsController) getStats(c *gin.Context) {
	onlineClients := job.GetLastOnlineClients()
	deltas := job.GetLastClientTrafficDeltas()

	var (
		stats []service.ClientStatResult
		err   error
	)
	for attempt := 0; attempt < 3; attempt++ {
		stats, err = a.statisticsService.GetAllStats(onlineClients, deltas)
		if err == nil {
			jsonObj(c, stats, nil)
			return
		}
		if !isDatabaseLockedError(err) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 150 * time.Millisecond)
	}

	jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.getStatsFail"), err)
}

func (a *StatisticsController) deleteStats(c *gin.Context) {
	err := a.statisticsService.DeleteAllStats()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.deleteStatsFail"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.statistics.toasts.deleteStatsSuccess"), nil)
}

func (a *StatisticsController) getIPInfo(c *gin.Context) {
	ip := strings.TrimSpace(c.Query("ip"))
	jsonObj(c, a.statisticsService.GetIPInfo(ip), nil)
}

func isDatabaseLockedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "database is locked")
}
