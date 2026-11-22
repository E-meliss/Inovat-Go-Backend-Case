package device

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxOrgIDLength    = 100
	maxNameLength     = 100
	maxPlatformLength = 50
)

type Repository interface {
	Create(ctx context.Context, d NewDevice, maxDevices int) (Device, error)
	List(ctx context.Context, orgID string) ([]Device, error)
	Get(ctx context.Context, orgID, id string) (Device, error)
	Delete(ctx context.Context, orgID, id string) error
	RecordHeartbeat(ctx context.Context, orgID, id string, seenAt time.Time) error
	MarkStaleOffline(ctx context.Context, cutoff time.Time) (int64, error)
}

type Service struct {
	repo             Repository
	maxDevicesPerOrg int
	heartbeatTimeout time.Duration
	now              func() time.Time
}

func NewService(repo Repository, maxDevicesPerOrg int, heartbeatTimeout time.Duration, now func() time.Time) *Service {
	return &Service{
		repo:             repo,
		maxDevicesPerOrg: maxDevicesPerOrg,
		heartbeatTimeout: heartbeatTimeout,
		now:              now,
	}
}

func (s *Service) Register(ctx context.Context, orgID, name, platform string) (Device, error) {
	orgID, err := normalizeOrgID(orgID)
	if err != nil {
		return Device{}, err
	}
	d := NewDevice{
		OrgID:    orgID,
		Name:     strings.TrimSpace(name),
		Platform: strings.TrimSpace(platform),
	}
	if err := validateField("name", d.Name, maxNameLength); err != nil {
		return Device{}, err
	}
	if err := validateField("platform", d.Platform, maxPlatformLength); err != nil {
		return Device{}, err
	}
	return s.repo.Create(ctx, d, s.maxDevicesPerOrg)
}

func (s *Service) List(ctx context.Context, orgID string) ([]Device, error) {
	orgID, err := normalizeOrgID(orgID)
	if err != nil {
		return nil, err
	}
	return s.repo.List(ctx, orgID)
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Device, error) {
	orgID, err := normalizeOrgID(orgID)
	if err != nil {
		return Device{}, err
	}
	return s.repo.Get(ctx, orgID, id)
}

func (s *Service) Delete(ctx context.Context, orgID, id string) error {
	orgID, err := normalizeOrgID(orgID)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, orgID, id)
}

func (s *Service) RecordHeartbeat(ctx context.Context, orgID, id string) error {
	orgID, err := normalizeOrgID(orgID)
	if err != nil {
		return err
	}
	return s.repo.RecordHeartbeat(ctx, orgID, id, s.now())
}

func (s *Service) MarkStaleOffline(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-s.heartbeatTimeout)
	return s.repo.MarkStaleOffline(ctx, cutoff)
}

func normalizeOrgID(orgID string) (string, error) {
	orgID = strings.TrimSpace(orgID)
	if err := validateField("org id", orgID, maxOrgIDLength); err != nil {
		return "", err
	}
	return orgID, nil
}

func validateField(name, value string, maxLength int) error {
	if value == "" {
		return &InvalidInputError{Message: name + " is required"}
	}
	if strings.ContainsRune(value, 0) {
		return &InvalidInputError{Message: name + " must not contain null characters"}
	}
	if utf8.RuneCountInString(value) > maxLength {
		return &InvalidInputError{Message: fmt.Sprintf("%s must be at most %d characters", name, maxLength)}
	}
	return nil
}
