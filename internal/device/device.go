package device

import "time"

type Status string

const (
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
)

type Device struct {
	ID         string
	OrgID      string
	Name       string
	Platform   string
	Status     Status
	LastSeenAt *time.Time
	CreatedAt  time.Time
}

type NewDevice struct {
	OrgID    string
	Name     string
	Platform string
}
