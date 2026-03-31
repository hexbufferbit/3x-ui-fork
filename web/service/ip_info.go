package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v2/logger"
)

const (
	ipInfoCacheTTL      = 12 * time.Hour
	ipInfoFailureTTL    = 10 * time.Minute
	ipLookupTimeout     = 5 * time.Second
	ipLookupServiceBase = "https://ipwho.is/"
)

type ClientIPInfo struct {
	IP           string `json:"ip"`
	ISP          string `json:"isp"`
	Organization string `json:"organization"`
	City         string `json:"city"`
	Region       string `json:"region"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Message      string `json:"message"`
}

type cachedClientIPInfo struct {
	info      ClientIPInfo
	expiresAt time.Time
}

type ipWhoIsResponse struct {
	Success    bool   `json:"success"`
	IP         string `json:"ip"`
	City       string `json:"city"`
	Region     string `json:"region"`
	Country    string `json:"country"`
	Message    string `json:"message"`
	Connection struct {
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Timezone struct {
		ID string `json:"id"`
	} `json:"timezone"`
}

type ipInfoIOResponse struct {
	IP       string `json:"ip"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Org      string `json:"org"`
	Timezone string `json:"timezone"`
}

var (
	ipInfoCache   = map[string]cachedClientIPInfo{}
	ipInfoCacheMu sync.RWMutex
	ipHTTPClient  = &http.Client{Timeout: ipLookupTimeout}
)

func (s *StatisticsService) GetIPInfo(ip string) ClientIPInfo {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ClientIPInfo{
			IP:      ip,
			Message: "Invalid IP address",
		}
	}

	if !shouldLookupPublicIP(addr) {
		return ClientIPInfo{
			IP:      ip,
			Message: "Private or reserved IP address",
		}
	}

	if cached, ok := getCachedIPInfo(ip); ok {
		return cached
	}

	lookupURL := ipLookupServiceBase + url.PathEscape(ip)
	req, err := http.NewRequest(http.MethodGet, lookupURL, nil)
	if err != nil {
		return cacheIPInfo(ip, ClientIPInfo{
			IP:      ip,
			Message: "IP information unavailable",
		}, ipInfoFailureTTL)
	}

	resp, err := ipHTTPClient.Do(req)
	if err != nil {
		logger.Warning("IP lookup failed for", ip, ":", err)
		return cacheIPInfo(ip, ClientIPInfo{
			IP:      ip,
			Message: "IP information unavailable",
		}, ipInfoFailureTTL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warning("IP lookup returned status", resp.StatusCode, "for", ip)
		return cacheIPInfo(ip, ClientIPInfo{
			IP:      ip,
			Message: fmt.Sprintf("IP information unavailable (status %d)", resp.StatusCode),
		}, ipInfoFailureTTL)
	}

	var payload ipWhoIsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		logger.Warning("IP lookup decode failed for", ip, ":", err)
		return cacheIPInfo(ip, ClientIPInfo{
			IP:      ip,
			Message: "IP information unavailable",
		}, ipInfoFailureTTL)
	}

	if !payload.Success {
		message := payload.Message
		if message == "" {
			message = "IP information unavailable"
		}
		info, fallbackErr := lookupIPInfoWithIPInfo(ip)
		if fallbackErr == nil {
			return cacheIPInfo(ip, info, ipInfoCacheTTL)
		}
		return cacheIPInfo(ip, ClientIPInfo{
			IP:      ip,
			Message: message,
		}, ipInfoFailureTTL)
	}

	info := ClientIPInfo{
		IP:           payload.IP,
		ISP:          payload.Connection.ISP,
		Organization: payload.Connection.Org,
		City:         payload.City,
		Region:       payload.Region,
		Country:      payload.Country,
		Timezone:     payload.Timezone.ID,
	}
	if info.IP == "" {
		info.IP = ip
	}
	if info.ISP == "" || info.Organization == "" || info.City == "" || info.Region == "" || info.Country == "" || info.Timezone == "" {
		fillIPInfoFromFallback(&info, ip)
	}

	if info.ISP == "" && info.Organization != "" {
		info.ISP = info.Organization
	}
	if info.Organization == "" && info.ISP != "" {
		info.Organization = info.ISP
	}

	return cacheIPInfo(ip, info, ipInfoCacheTTL)
}

func getCachedIPInfo(ip string) (ClientIPInfo, bool) {
	ipInfoCacheMu.RLock()
	defer ipInfoCacheMu.RUnlock()

	cached, ok := ipInfoCache[ip]
	if !ok || time.Now().After(cached.expiresAt) {
		return ClientIPInfo{}, false
	}

	return cached.info, true
}

func cacheIPInfo(ip string, info ClientIPInfo, ttl time.Duration) ClientIPInfo {
	ipInfoCacheMu.Lock()
	defer ipInfoCacheMu.Unlock()

	ipInfoCache[ip] = cachedClientIPInfo{
		info:      info,
		expiresAt: time.Now().Add(ttl),
	}
	return info
}

func shouldLookupPublicIP(addr netip.Addr) bool {
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func fillIPInfoFromFallback(info *ClientIPInfo, ip string) {
	fallback, err := lookupIPInfoWithIPInfo(ip)
	if err != nil {
		return
	}

	if info.IP == "" {
		info.IP = fallback.IP
	}
	if info.ISP == "" {
		info.ISP = fallback.ISP
	}
	if info.Organization == "" {
		info.Organization = fallback.Organization
	}
	if info.City == "" {
		info.City = fallback.City
	}
	if info.Region == "" {
		info.Region = fallback.Region
	}
	if info.Country == "" {
		info.Country = fallback.Country
	}
	if info.Timezone == "" {
		info.Timezone = fallback.Timezone
	}
}

func lookupIPInfoWithIPInfo(ip string) (ClientIPInfo, error) {
	lookupURL := "https://ipinfo.io/" + url.PathEscape(ip) + "/json"
	req, err := http.NewRequest(http.MethodGet, lookupURL, nil)
	if err != nil {
		return ClientIPInfo{}, err
	}

	resp, err := ipHTTPClient.Do(req)
	if err != nil {
		return ClientIPInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ClientIPInfo{}, fmt.Errorf("ipinfo status %d", resp.StatusCode)
	}

	var payload ipInfoIOResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ClientIPInfo{}, err
	}

	return ClientIPInfo{
		IP:           payload.IP,
		ISP:          payload.Org,
		Organization: payload.Org,
		City:         payload.City,
		Region:       payload.Region,
		Country:      payload.Country,
		Timezone:     payload.Timezone,
	}, nil
}
