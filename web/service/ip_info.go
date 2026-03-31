package service

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v2/logger"
)

const (
	ipInfoCacheTTL              = 12 * time.Hour
	ipInfoFailureTTL            = 10 * time.Minute
	ipLookupTimeout             = 5 * time.Second
	whatIsMyIPAddressLookupBase = "https://whatismyipaddress.com/ip/"
	ipWhoIsLookupBase           = "https://ipwho.is/"
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
	ipInfoCache              = map[string]cachedClientIPInfo{}
	ipInfoCacheMu            sync.RWMutex
	ipHTTPClient             = &http.Client{Timeout: ipLookupTimeout}
	htmlTagRegex             = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlWhitespaceRegex      = regexp.MustCompile(`\s+`)
	ipDetailInformationRegex = regexp.MustCompile(`(?is)<p[^>]*class=["'][^"']*information[^"']*["'][^>]*>\s*<span>\s*([^<]+?)\s*</span>\s*<span>(.*?)</span>\s*</p>`)
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

	info, lookupErr := lookupIPInfoWithWhatIsMyIPAddress(ip)
	if lookupErr == nil {
		enrichMissingIPInfo(&info, ip, lookupIPInfoWithIPWhoIs, lookupIPInfoWithIPInfo)
		return cacheIPInfo(ip, normalizeIPInfo(info, ip), ipInfoCacheTTL)
	}
	logger.Warning("IP lookup failed for", ip, "via whatismyipaddress.com:", lookupErr)

	info, lookupErr = lookupIPInfoWithIPWhoIs(ip)
	if lookupErr == nil {
		enrichMissingIPInfo(&info, ip, lookupIPInfoWithIPInfo)
		return cacheIPInfo(ip, normalizeIPInfo(info, ip), ipInfoCacheTTL)
	}
	logger.Warning("IP lookup failed for", ip, "via ipwho.is:", lookupErr)

	info, lookupErr = lookupIPInfoWithIPInfo(ip)
	if lookupErr == nil {
		return cacheIPInfo(ip, normalizeIPInfo(info, ip), ipInfoCacheTTL)
	}
	logger.Warning("IP lookup failed for", ip, "via ipinfo.io:", lookupErr)

	return cacheIPInfo(ip, ClientIPInfo{
		IP:      ip,
		Message: "IP information unavailable",
	}, ipInfoFailureTTL)
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

func enrichMissingIPInfo(info *ClientIPInfo, ip string, lookups ...func(string) (ClientIPInfo, error)) {
	for _, lookup := range lookups {
		if !needsIPInfoEnrichment(*info) {
			return
		}

		fallback, err := lookup(ip)
		if err != nil {
			continue
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
}

func needsIPInfoEnrichment(info ClientIPInfo) bool {
	return info.ISP == "" || info.Organization == "" || info.City == "" || info.Region == "" || info.Country == "" || info.Timezone == ""
}

func normalizeIPInfo(info ClientIPInfo, ip string) ClientIPInfo {
	if info.IP == "" {
		info.IP = ip
	}
	if info.ISP == "" && info.Organization != "" {
		info.ISP = info.Organization
	}
	if info.Organization == "" && info.ISP != "" {
		info.Organization = info.ISP
	}
	return info
}

func lookupIPInfoWithWhatIsMyIPAddress(ip string) (ClientIPInfo, error) {
	req, err := newBrowserLikeRequest(http.MethodGet, whatIsMyIPAddressLookupBase+url.PathEscape(ip))
	if err != nil {
		return ClientIPInfo{}, err
	}

	resp, err := ipHTTPClient.Do(req)
	if err != nil {
		return ClientIPInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ClientIPInfo{}, fmt.Errorf("whatismyipaddress status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ClientIPInfo{}, err
	}

	fields := parseWhatIsMyIPAddressFields(string(body))
	info := ClientIPInfo{
		IP:           firstNonEmpty(fields["Hostname"], fields["IP"], ip),
		ISP:          fields["ISP"],
		Organization: fields["ISP"],
		City:         fields["City"],
		Region:       fields["State/Region"],
		Country:      fields["Country"],
	}

	if info.ISP == "" && info.City == "" && info.Region == "" && info.Country == "" {
		return ClientIPInfo{}, fmt.Errorf("whatismyipaddress returned no IP details")
	}

	return info, nil
}

func newBrowserLikeRequest(method, lookupURL string) (*http.Request, error) {
	req, err := http.NewRequest(method, lookupURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	return req, nil
}

func parseWhatIsMyIPAddressFields(page string) map[string]string {
	fields := map[string]string{}
	for _, match := range ipDetailInformationRegex.FindAllStringSubmatch(page, -1) {
		if len(match) < 3 {
			continue
		}
		label := normalizeHTMLText(strings.TrimSuffix(match[1], ":"))
		value := normalizeHTMLText(match[2])
		if label == "" || value == "" {
			continue
		}
		fields[label] = value
	}
	return fields
}

func normalizeHTMLText(value string) string {
	value = htmlTagRegex.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = htmlWhitespaceRegex.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lookupIPInfoWithIPWhoIs(ip string) (ClientIPInfo, error) {
	req, err := http.NewRequest(http.MethodGet, ipWhoIsLookupBase+url.PathEscape(ip), nil)
	if err != nil {
		return ClientIPInfo{}, err
	}

	resp, err := ipHTTPClient.Do(req)
	if err != nil {
		return ClientIPInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ClientIPInfo{}, fmt.Errorf("ipwho status %d", resp.StatusCode)
	}

	var payload ipWhoIsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ClientIPInfo{}, err
	}

	if !payload.Success {
		message := payload.Message
		if message == "" {
			message = "IP information unavailable"
		}
		return ClientIPInfo{}, fmt.Errorf("%s", message)
	}

	return ClientIPInfo{
		IP:           payload.IP,
		ISP:          payload.Connection.ISP,
		Organization: payload.Connection.Org,
		City:         payload.City,
		Region:       payload.Region,
		Country:      payload.Country,
		Timezone:     payload.Timezone.ID,
	}, nil
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
