package job

import (
	"sync"

	"github.com/mhsanaei/3x-ui/v2/xray"
)

var (
	lastClientTrafficDeltas []*xray.ClientTraffic
	lastOnlineClients       []string
	deltaMu                 sync.RWMutex
)

// SetLastClientTrafficDeltas stores the latest traffic deltas for bandwidth queries.
func SetLastClientTrafficDeltas(deltas []*xray.ClientTraffic) {
	deltaMu.Lock()
	defer deltaMu.Unlock()
	lastClientTrafficDeltas = deltas
}

// GetLastClientTrafficDeltas returns the latest traffic deltas.
func GetLastClientTrafficDeltas() []*xray.ClientTraffic {
	deltaMu.RLock()
	defer deltaMu.RUnlock()
	return lastClientTrafficDeltas
}

// SetLastOnlineClients stores the latest online clients list.
func SetLastOnlineClients(clients []string) {
	deltaMu.Lock()
	defer deltaMu.Unlock()
	lastOnlineClients = clients
}

// GetLastOnlineClients returns the latest online clients list.
func GetLastOnlineClients() []string {
	deltaMu.RLock()
	defer deltaMu.RUnlock()
	return lastOnlineClients
}
