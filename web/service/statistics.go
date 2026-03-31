package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/xray"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StatisticsService struct{}

// ClientStatResult holds aggregated statistics for a single client for the API response.
type ClientStatResult struct {
	Email         string           `json:"email"`
	TotalUptime   int64            `json:"totalUptime"`   // seconds
	TotalUpload   int64            `json:"totalUpload"`   // bytes (allTime up)
	TotalDownload int64            `json:"totalDownload"` // bytes (allTime down)
	AllTimeUsage  int64            `json:"allTimeUsage"`  // bytes
	IsOnline      bool             `json:"isOnline"`
	CurrentBwUp   int64            `json:"currentBwUp"`   // bytes/sec
	CurrentBwDown int64            `json:"currentBwDown"` // bytes/sec
	LastOnline    int64            `json:"lastOnline"`    // unix ms
	IPs           []ClientIPResult `json:"ips"`           // currently active client IP addresses
}

// RecordOnlineClients increments uptime for all currently online clients.
// interval is the number of seconds since the last call (typically 10s).
func (s *StatisticsService) RecordOnlineClients(onlineClients []string, intervalSec int64) {
	if len(onlineClients) == 0 {
		return
	}
	db := database.GetDB()
	now := time.Now().UnixMilli()

	for _, email := range onlineClients {
		stat := model.ClientUptimeStat{
			Email:       email,
			TotalUptime: intervalSec,
			LastUpdated: now,
		}
		err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "email"}},
			DoUpdates: clause.Assignments(map[string]any{
				"total_uptime": gorm.Expr("total_uptime + ?", intervalSec),
				"last_updated": now,
			}),
		}).Create(&stat).Error
		if err != nil {
			logger.Warning("RecordOnlineClients upsert failed for", email, ":", err)
		}
	}
}

// GetAllStats returns aggregated statistics for all clients.
func (s *StatisticsService) GetAllStats(onlineClients []string, clientTrafficDeltas []*xray.ClientTraffic) ([]ClientStatResult, error) {
	db := database.GetDB()

	// Get all uptime stats
	var uptimeStats []model.ClientUptimeStat
	err := db.Find(&uptimeStats).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	uptimeMap := make(map[string]int64, len(uptimeStats))
	for _, u := range uptimeStats {
		uptimeMap[u.Email] = u.TotalUptime
	}

	// Get all client traffic for totals
	var clientTraffics []xray.ClientTraffic
	err = db.Find(&clientTraffics).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// Build online set
	onlineSet := make(map[string]bool, len(onlineClients))
	for _, e := range onlineClients {
		onlineSet[e] = true
	}

	// Build bandwidth delta map (from last traffic cycle)
	bwUpMap := make(map[string]int64)
	bwDownMap := make(map[string]int64)
	for _, ct := range clientTrafficDeltas {
		bwUpMap[ct.Email] = ct.Up // bytes in last 10s interval
		bwDownMap[ct.Email] = ct.Down
	}

	// Build client IP map
	var clientIps []model.InboundClientIps
	err = db.Find(&clientIps).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	now := time.Now()
	ipMap := make(map[string][]ClientIPResult, len(clientIps))
	for _, cip := range clientIps {
		entries, err := ParseStoredClientIPs(cip.Ips)
		if err != nil {
			logger.Warning("failed to parse stored IPs for", cip.ClientEmail, ":", err)
			continue
		}
		ipMap[cip.ClientEmail] = BuildClientIPResults(entries, onlineSet[cip.ClientEmail], now)
	}

	// Merge into results
	results := make([]ClientStatResult, 0, len(clientTraffics))
	for _, ct := range clientTraffics {
		r := ClientStatResult{
			Email:         ct.Email,
			TotalUptime:   uptimeMap[ct.Email],
			TotalUpload:   ct.Up,
			TotalDownload: ct.Down,
			AllTimeUsage:  ct.AllTime,
			IsOnline:      onlineSet[ct.Email],
			LastOnline:    ct.LastOnline,
			IPs:           ipMap[ct.Email],
		}
		// Compute bandwidth as bytes/sec (delta / 10s interval)
		if onlineSet[ct.Email] {
			r.CurrentBwUp = bwUpMap[ct.Email] / 10
			r.CurrentBwDown = bwDownMap[ct.Email] / 10
		}
		results = append(results, r)
	}

	return results, nil
}

// DeleteAllStats hard-deletes uptime/IP statistics and clears related logs.
func (s *StatisticsService) DeleteAllStats() error {
	return s.deleteStatisticsData(true)
}

// DeleteTrackedStats removes uptime/IP statistics and the access-log state tied to IP tracking.
func (s *StatisticsService) DeleteTrackedStats() error {
	return s.deleteStatisticsData(false)
}

func (s *StatisticsService) deleteStatisticsData(clearErrorLog bool) error {
	db := database.GetDB()
	tx := db.Begin()
	if err := tx.Where("1 = 1").Delete(&model.ClientUptimeStat{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Where("1 = 1").Delete(&model.InboundClientIps{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}

	clearOps := []error{
		clearConfiguredLogFile(xray.GetAccessLogPath, removeFileIfExists),
		removeFileIfExists(xray.GetAccessPersistentLogPath()),
		removeFileIfExists(xray.GetAccessPersistentPrevLogPath()),
	}
	if clearErrorLog {
		clearOps = append(clearOps, clearConfiguredLogFile(xray.GetErrorLogPath, truncateFileIfExists))
	}

	return errors.Join(clearOps...)
}

// GetUptimeStats returns only uptime data for all clients (for bot/lightweight queries).
func (s *StatisticsService) GetUptimeStats() ([]model.ClientUptimeStat, error) {
	db := database.GetDB()
	var stats []model.ClientUptimeStat
	err := db.Find(&stats).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return stats, nil
}

func clearConfiguredLogFile(getPath func() (string, error), clear func(string) error) error {
	path, err := getPath()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return clear(path)
}

func truncateFileIfExists(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || strings.EqualFold(path, "none") {
		return nil
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}

	return os.Truncate(path, 0)
}

func removeFileIfExists(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || strings.EqualFold(path, "none") {
		return nil
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}

	return os.Remove(path)
}
