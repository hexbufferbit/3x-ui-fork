package job

import (
	"time"

	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

type StatisticsCleanupJob struct {
	settingService    service.SettingService
	statisticsService service.StatisticsService
}

func NewStatisticsCleanupJob() *StatisticsCleanupJob {
	return new(StatisticsCleanupJob)
}

func (j *StatisticsCleanupJob) Run() {
	hours, err := j.settingService.GetStatisticsAutoDeleteHours()
	if err != nil {
		logger.Warning("failed to read statistics auto delete hours:", err)
		return
	}
	if hours <= 0 {
		return
	}

	lastRun, err := j.settingService.GetStatisticsAutoDeleteLastRun()
	if err != nil {
		logger.Warning("failed to read statistics auto delete last run:", err)
		return
	}

	now := time.Now().Unix()
	if lastRun <= 0 {
		if err := j.settingService.SetStatisticsAutoDeleteLastRun(now); err != nil {
			logger.Warning("failed to initialize statistics auto delete last run:", err)
		}
		return
	}

	if now-lastRun < int64(hours)*3600 {
		return
	}

	if err := j.statisticsService.DeleteTrackedStats(); err != nil {
		logger.Warning("failed to auto delete statistics:", err)
		return
	}
	if err := j.settingService.SetStatisticsAutoDeleteLastRun(now); err != nil {
		logger.Warning("failed to update statistics auto delete last run:", err)
	}
}
