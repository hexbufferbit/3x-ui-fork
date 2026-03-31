package model

// ClientUptimeStat tracks cumulative connection uptime and bandwidth statistics per client.
type ClientUptimeStat struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Email       string `json:"email" gorm:"uniqueIndex"`
	TotalUptime int64  `json:"totalUptime" gorm:"default:0"` // Total uptime in seconds
	LastUpdated int64  `json:"lastUpdated" gorm:"default:0"` // Last update timestamp (unix ms)
}
