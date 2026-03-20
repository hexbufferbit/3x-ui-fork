package job

import (
	"github.com/mhsanaei/3x-ui/v2/web/service"
)

// CheckUsageWarningJob periodically checks client usage against configured
// thresholds and sends Telegram warnings when thresholds are crossed.
type CheckUsageWarningJob struct {
	tgbotService service.Tgbot
	xrayService  service.XrayService
}

// NewCheckUsageWarningJob creates a new usage warning check job instance.
func NewCheckUsageWarningJob() *CheckUsageWarningJob {
	return new(CheckUsageWarningJob)
}

// Run checks usage warnings if Xray is running and the Telegram bot is active.
func (j *CheckUsageWarningJob) Run() {
	if !j.xrayService.IsXrayRunning() {
		return
	}
	j.tgbotService.CheckUsageWarnings()
}
