package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/web/job"
	"github.com/mhsanaei/3x-ui/v2/web/service"

	"github.com/gin-gonic/gin"
)

type StatisticsController struct {
	BaseController
	statisticsService service.StatisticsService
	settingService    service.SettingService
}

func NewStatisticsController(g *gin.RouterGroup) *StatisticsController {
	a := &StatisticsController{}
	a.initRouter(g)
	return a
}

func (a *StatisticsController) initRouter(g *gin.RouterGroup) {
	g.GET("/stats", a.getStats)
	g.GET("/ipInfo", a.getIPInfo)
	g.GET("/autoDeleteConfig", a.getAutoDeleteConfig)
	g.POST("/deleteStats", a.deleteStats)
	g.POST("/autoDeleteConfig", a.updateAutoDeleteConfig)
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

func (a *StatisticsController) getAutoDeleteConfig(c *gin.Context) {
	hours, err := a.settingService.GetStatisticsAutoDeleteHours()
	if err != nil {
		jsonMsg(c, "Failed to load auto delete settings", err)
		return
	}
	lastRun, err := a.settingService.GetStatisticsAutoDeleteLastRun()
	if err != nil {
		jsonMsg(c, "Failed to load auto delete settings", err)
		return
	}

	jsonObj(c, gin.H{
		"hours":   hours,
		"lastRun": lastRun * 1000,
	}, nil)
}

func (a *StatisticsController) updateAutoDeleteConfig(c *gin.Context) {
	var payload struct {
		Hours int `json:"hours"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, "Invalid auto delete configuration", err)
		return
	}
	if !isValidStatisticsAutoDeleteInterval(payload.Hours) {
		jsonMsg(c, "Invalid auto delete interval", strconv.ErrSyntax)
		return
	}

	lastRun := int64(0)
	if payload.Hours > 0 {
		lastRun = time.Now().Unix()
	}

	if err := a.settingService.SetStatisticsAutoDeleteHours(payload.Hours); err != nil {
		jsonMsg(c, "Failed to save auto delete settings", err)
		return
	}
	if err := a.settingService.SetStatisticsAutoDeleteLastRun(lastRun); err != nil {
		jsonMsg(c, "Failed to save auto delete settings", err)
		return
	}

	jsonObj(c, gin.H{
		"hours":   payload.Hours,
		"lastRun": lastRun * 1000,
	}, nil)
}

func isDatabaseLockedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "database is locked")
}

func isValidStatisticsAutoDeleteInterval(hours int) bool {
	switch hours {
	case 0, 24, 48, 72, 168:
		return true
	default:
		return false
	}
}
