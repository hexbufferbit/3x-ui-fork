package service

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const clientIPActiveWindow = 60 * time.Second

type StoredClientIP struct {
	IP        string `json:"ip"`
	Timestamp int64  `json:"timestamp"`
}

type ClientIPResult struct {
	IP       string `json:"ip"`
	LastSeen int64  `json:"lastSeen"`
}

func ParseStoredClientIPs(raw string) ([]StoredClientIP, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var entries []StoredClientIP
	if err := json.Unmarshal([]byte(raw), &entries); err == nil {
		return normalizeStoredClientIPs(entries), nil
	}

	var legacyEntries []string
	if err := json.Unmarshal([]byte(raw), &legacyEntries); err == nil {
		entries = make([]StoredClientIP, 0, len(legacyEntries))
		for _, ip := range legacyEntries {
			entries = append(entries, StoredClientIP{IP: ip})
		}
		return normalizeStoredClientIPs(entries), nil
	}

	var legacyObjects []struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal([]byte(raw), &legacyObjects); err != nil {
		return nil, err
	}

	entries = make([]StoredClientIP, 0, len(legacyObjects))
	for _, entry := range legacyObjects {
		entries = append(entries, StoredClientIP{IP: entry.IP})
	}
	return normalizeStoredClientIPs(entries), nil
}

func MergeStoredClientIPs(existing []StoredClientIP, fresh []StoredClientIP) []StoredClientIP {
	merged := make([]StoredClientIP, 0, len(existing)+len(fresh))
	merged = append(merged, existing...)
	merged = append(merged, fresh...)
	return normalizeStoredClientIPs(merged)
}

func PruneActiveClientIPs(entries []StoredClientIP, clientOnline bool, now time.Time) []StoredClientIP {
	normalized := normalizeStoredClientIPs(entries)
	if len(normalized) == 0 {
		return nil
	}

	cutoff := now.Add(-clientIPActiveWindow).Unix()
	active := make([]StoredClientIP, 0, len(normalized))
	for _, entry := range normalized {
		if entry.Timestamp == 0 || entry.Timestamp < cutoff {
			continue
		}
		active = append(active, entry)
	}

	if len(active) == 0 && clientOnline {
		return []StoredClientIP{normalized[0]}
	}

	return active
}

func BuildClientIPResults(entries []StoredClientIP, clientOnline bool, now time.Time) []ClientIPResult {
	activeEntries := PruneActiveClientIPs(entries, clientOnline, now)
	results := make([]ClientIPResult, 0, len(activeEntries))
	for _, entry := range activeEntries {
		results = append(results, ClientIPResult{
			IP:       entry.IP,
			LastSeen: entry.Timestamp * 1000,
		})
	}
	return results
}

func normalizeStoredClientIPs(entries []StoredClientIP) []StoredClientIP {
	if len(entries) == 0 {
		return nil
	}

	deduped := make(map[string]int64, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		ip := strings.TrimSpace(entry.IP)
		if ip == "" {
			continue
		}

		if existing, ok := deduped[ip]; ok {
			if entry.Timestamp > existing {
				deduped[ip] = entry.Timestamp
			}
			continue
		}

		deduped[ip] = entry.Timestamp
		order = append(order, ip)
	}

	normalized := make([]StoredClientIP, 0, len(order))
	for _, ip := range order {
		normalized = append(normalized, StoredClientIP{
			IP:        ip,
			Timestamp: deduped[ip],
		})
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].Timestamp > normalized[j].Timestamp
	})

	return normalized
}
