package job

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

// CheckClientIpJob monitors client IP addresses from access logs and manages IP blocking based on configured limits.
type CheckClientIpJob struct {
	lastClear     int64
	disAllowedIps []string
}

var job *CheckClientIpJob

// NewCheckClientIpJob creates a new client IP monitoring job instance.
func NewCheckClientIpJob() *CheckClientIpJob {
	job = new(CheckClientIpJob)
	return job
}

func (j *CheckClientIpJob) Run() {
	if j.lastClear == 0 {
		j.lastClear = time.Now().Unix()
	}

	shouldClearAccessLog := false
	iplimitActive := j.hasLimitIp()
	isAccessLogAvailable := j.checkAccessLogAvailable(iplimitActive)

	if isAccessLogAvailable {
		enforceIPLimit := iplimitActive
		if runtime.GOOS != "windows" && iplimitActive && !j.checkFail2BanInstalled() {
			enforceIPLimit = false
			logger.Warning("[LimitIP] Fail2Ban is not installed, IP history will still be collected but IP limit enforcement is disabled.")
		}
		shouldClearAccessLog = j.processLogFile(enforceIPLimit)
	}

	j.pruneInactiveClientIPs()

	if shouldClearAccessLog || (isAccessLogAvailable && time.Now().Unix()-j.lastClear > 3600) {
		j.clearAccessLog()
	}
}

func (j *CheckClientIpJob) clearAccessLog() {
	logAccessP, err := os.OpenFile(xray.GetAccessPersistentLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	j.checkError(err)
	defer logAccessP.Close()

	accessLogPath, err := xray.GetAccessLogPath()
	j.checkError(err)

	file, err := os.Open(accessLogPath)
	j.checkError(err)
	defer file.Close()

	_, err = io.Copy(logAccessP, file)
	j.checkError(err)

	err = os.Truncate(accessLogPath, 0)
	j.checkError(err)

	j.lastClear = time.Now().Unix()
}

func (j *CheckClientIpJob) hasLimitIp() bool {
	db := database.GetDB()
	var inbounds []*model.Inbound

	err := db.Model(model.Inbound{}).Find(&inbounds).Error
	if err != nil {
		return false
	}

	for _, inbound := range inbounds {
		if inbound.Settings == "" {
			continue
		}

		settings := map[string][]model.Client{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		clients := settings["clients"]

		for _, client := range clients {
			limitIp := client.LimitIP
			if limitIp > 0 {
				return true
			}
		}
	}

	return false
}

func (j *CheckClientIpJob) processLogFile(enforceIPLimit bool) bool {

	ipRegex := regexp.MustCompile(`from (?:tcp:|udp:)?\[?([0-9a-fA-F\.:]+)\]?:\d+ accepted`)
	emailRegex := regexp.MustCompile(`email: (.+)$`)
	timestampRegex := regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)

	accessLogPath, _ := xray.GetAccessLogPath()
	file, _ := os.Open(accessLogPath)
	defer file.Close()

	// Track IPs with their last seen timestamp
	inboundClientIps := make(map[string]map[string]int64, 100)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		ipMatches := ipRegex.FindStringSubmatch(line)
		if len(ipMatches) < 2 {
			continue
		}

		ip := ipMatches[1]

		if ip == "127.0.0.1" || ip == "::1" {
			continue
		}

		emailMatches := emailRegex.FindStringSubmatch(line)
		if len(emailMatches) < 2 {
			continue
		}
		email := emailMatches[1]

		// Extract timestamp from log line
		var timestamp int64
		timestampMatches := timestampRegex.FindStringSubmatch(line)
		if len(timestampMatches) >= 2 {
			t, err := time.Parse("2006/01/02 15:04:05", timestampMatches[1])
			if err == nil {
				timestamp = t.Unix()
			} else {
				timestamp = time.Now().Unix()
			}
		} else {
			timestamp = time.Now().Unix()
		}

		if _, exists := inboundClientIps[email]; !exists {
			inboundClientIps[email] = make(map[string]int64)
		}
		// Update timestamp - keep the latest
		if existingTime, ok := inboundClientIps[email][ip]; !ok || timestamp > existingTime {
			inboundClientIps[email][ip] = timestamp
		}
	}

	shouldCleanLog := false
	for email, ipTimestamps := range inboundClientIps {

		// Convert to IPWithTimestamp slice
		ipsWithTime := make([]service.StoredClientIP, 0, len(ipTimestamps))
		for ip, timestamp := range ipTimestamps {
			ipsWithTime = append(ipsWithTime, service.StoredClientIP{IP: ip, Timestamp: timestamp})
		}

		clientIpsRecord, err := j.getInboundClientIps(email)
		if err != nil {
			j.addInboundClientIps(email, ipsWithTime)
			continue
		}

		shouldCleanLog = j.updateInboundClientIps(clientIpsRecord, email, ipsWithTime, enforceIPLimit) || shouldCleanLog
	}

	return shouldCleanLog
}

func (j *CheckClientIpJob) checkFail2BanInstalled() bool {
	cmd := "fail2ban-client"
	args := []string{"-h"}
	err := exec.Command(cmd, args...).Run()
	return err == nil
}

func (j *CheckClientIpJob) checkAccessLogAvailable(iplimitActive bool) bool {
	accessLogPath, err := xray.GetAccessLogPath()
	if err != nil {
		return false
	}

	if accessLogPath == "none" || accessLogPath == "" {
		if iplimitActive {
			logger.Warning("[LimitIP] Access log path is not set, Please configure the access log path in Xray configs.")
		}
		return false
	}

	return true
}

func (j *CheckClientIpJob) checkError(e error) {
	if e != nil {
		logger.Warning("client ip job err:", e)
	}
}

func (j *CheckClientIpJob) getInboundClientIps(clientEmail string) (*model.InboundClientIps, error) {
	db := database.GetDB()
	InboundClientIps := &model.InboundClientIps{}
	err := db.Model(model.InboundClientIps{}).Where("client_email = ?", clientEmail).First(InboundClientIps).Error
	if err != nil {
		return nil, err
	}
	return InboundClientIps, nil
}

func (j *CheckClientIpJob) addInboundClientIps(clientEmail string, ipsWithTime []service.StoredClientIP) error {
	inboundClientIps := &model.InboundClientIps{}
	jsonIps, err := json.Marshal(ipsWithTime)
	j.checkError(err)

	inboundClientIps.ClientEmail = clientEmail
	inboundClientIps.Ips = string(jsonIps)

	db := database.GetDB()
	tx := db.Begin()

	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	err = tx.Save(inboundClientIps).Error
	if err != nil {
		return err
	}
	return nil
}

func (j *CheckClientIpJob) updateInboundClientIps(inboundClientIps *model.InboundClientIps, clientEmail string, newIpsWithTime []service.StoredClientIP, enforceIPLimit bool) bool {
	// Get the inbound configuration
	inbound, err := j.getInboundByEmail(clientEmail)
	if err != nil {
		logger.Errorf("failed to fetch inbound settings for email %s: %s", clientEmail, err)
		return false
	}

	if inbound.Settings == "" {
		logger.Debug("wrong data:", inbound)
		return false
	}

	settings := map[string][]model.Client{}
	json.Unmarshal([]byte(inbound.Settings), &settings)
	clients := settings["clients"]

	// Find the client's IP limit
	var limitIp int
	var clientFound bool
	for _, client := range clients {
		if client.Email == clientEmail {
			limitIp = client.LimitIP
			clientFound = true
			break
		}
	}

	if !enforceIPLimit || !clientFound || limitIp <= 0 || !inbound.Enable {
		j.saveActiveClientIPs(inboundClientIps, clientEmail, newIpsWithTime)
		return false
	}

	oldIpsWithTime, err := service.ParseStoredClientIPs(inboundClientIps.Ips)
	if err != nil {
		logger.Warning("failed to parse existing client IPs for", clientEmail, ":", err)
	}

	mergedIps := service.MergeStoredClientIPs(oldIpsWithTime, newIpsWithTime)
	clientOnline := j.isClientOnline(clientEmail)
	activeIps := service.PruneActiveClientIPs(mergedIps, clientOnline, time.Now())

	shouldCleanLog := false
	j.disAllowedIps = []string{}

	// Open log file
	logIpFile, err := os.OpenFile(xray.GetIPLimitLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logger.Errorf("failed to open IP limit log file: %s", err)
		return false
	}
	defer logIpFile.Close()
	log.SetOutput(logIpFile)
	log.SetFlags(log.LstdFlags)

	// Check if we exceed the limit
	if len(activeIps) > limitIp {
		shouldCleanLog = true

		ipsForLimit := append([]service.StoredClientIP(nil), activeIps...)
		sort.SliceStable(ipsForLimit, func(i, j int) bool {
			return ipsForLimit[i].Timestamp < ipsForLimit[j].Timestamp
		})

		// Keep the oldest active IPs and ban the new excess ones.
		keptIps := ipsForLimit[:limitIp]
		bannedIps := ipsForLimit[limitIp:]

		// Log banned IPs in the format fail2ban filters expect: [LIMIT_IP] Email = X || Disconnecting OLD IP = Y || Timestamp = Z
		for _, ipTime := range bannedIps {
			j.disAllowedIps = append(j.disAllowedIps, ipTime.IP)
			log.Printf("[LIMIT_IP] Email = %s || Disconnecting OLD IP = %s || Timestamp = %d", clientEmail, ipTime.IP, ipTime.Timestamp)
		}

		if err := j.persistClientIPs(inboundClientIps, keptIps); err != nil {
			logger.Error("failed to save inboundClientIps:", err)
			return false
		}
	} else {
		// Under limit, save only currently active IPs.
		if err := j.persistClientIPs(inboundClientIps, activeIps); err != nil {
			logger.Error("failed to save inboundClientIps:", err)
			return false
		}
	}

	if len(j.disAllowedIps) > 0 {
		logger.Infof("[LIMIT_IP] Client %s: Kept %d current IPs, queued %d new IPs for fail2ban", clientEmail, limitIp, len(j.disAllowedIps))
	}

	return shouldCleanLog
}

func (j *CheckClientIpJob) pruneInactiveClientIPs() {
	db := database.GetDB()
	var records []model.InboundClientIps
	if err := db.Find(&records).Error; err != nil {
		logger.Warning("failed to load client IP records:", err)
		return
	}

	now := time.Now()
	for _, record := range records {
		entries, err := service.ParseStoredClientIPs(record.Ips)
		if err != nil {
			logger.Warning("failed to parse stored client IPs for", record.ClientEmail, ":", err)
			continue
		}

		activeIPs := service.PruneActiveClientIPs(entries, j.isClientOnline(record.ClientEmail), now)
		if err := j.persistClientIPs(&record, activeIPs); err != nil {
			logger.Warning("failed to prune client IPs for", record.ClientEmail, ":", err)
		}
	}
}

func (j *CheckClientIpJob) saveActiveClientIPs(inboundClientIps *model.InboundClientIps, clientEmail string, newIpsWithTime []service.StoredClientIP) {
	existingIps, err := service.ParseStoredClientIPs(inboundClientIps.Ips)
	if err != nil {
		logger.Warning("failed to parse existing client IPs for", clientEmail, ":", err)
	}

	activeIps := service.PruneActiveClientIPs(
		service.MergeStoredClientIPs(existingIps, newIpsWithTime),
		j.isClientOnline(clientEmail),
		time.Now(),
	)
	if err := j.persistClientIPs(inboundClientIps, activeIps); err != nil {
		logger.Warning("failed to save active client IPs for", clientEmail, ":", err)
	}
}

func (j *CheckClientIpJob) persistClientIPs(inboundClientIps *model.InboundClientIps, ips []service.StoredClientIP) error {
	db := database.GetDB()
	if len(ips) == 0 {
		return db.Delete(inboundClientIps).Error
	}

	jsonIps, err := json.Marshal(ips)
	if err != nil {
		return err
	}

	inboundClientIps.Ips = string(jsonIps)
	return db.Save(inboundClientIps).Error
}

func (j *CheckClientIpJob) isClientOnline(clientEmail string) bool {
	for _, email := range GetLastOnlineClients() {
		if email == clientEmail {
			return true
		}
	}
	return false
}

func (j *CheckClientIpJob) getInboundByEmail(clientEmail string) (*model.Inbound, error) {
	db := database.GetDB()
	inbound := &model.Inbound{}

	err := db.Model(&model.Inbound{}).Where("settings LIKE ?", "%"+clientEmail+"%").First(inbound).Error
	if err != nil {
		return nil, err
	}

	return inbound, nil
}
