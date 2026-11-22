package device

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubRepo struct {
	err error

	org           string
	created       NewDevice
	createdLimit  int
	heartbeatAt   time.Time
	staleCutoff   time.Time
	staleAffected int64
}

func (r *stubRepo) Create(_ context.Context, d NewDevice, maxDevices int) (Device, error) {
	r.created, r.createdLimit = d, maxDevices
	if r.err != nil {
		return Device{}, r.err
	}
	return Device{Name: d.Name, Platform: d.Platform, OrgID: d.OrgID}, nil
}

func (r *stubRepo) List(_ context.Context, orgID string) ([]Device, error) {
	r.org = orgID
	return nil, r.err
}

func (r *stubRepo) Get(_ context.Context, orgID, _ string) (Device, error) {
	r.org = orgID
	return Device{}, r.err
}

func (r *stubRepo) Delete(_ context.Context, orgID, _ string) error {
	r.org = orgID
	return r.err
}

func (r *stubRepo) RecordHeartbeat(_ context.Context, orgID, _ string, seenAt time.Time) error {
	r.org = orgID
	r.heartbeatAt = seenAt
	return r.err
}

func (r *stubRepo) MarkStaleOffline(_ context.Context, cutoff time.Time) (int64, error) {
	r.staleCutoff = cutoff
	return r.staleAffected, r.err
}

var fixedNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func newTestService(repo *stubRepo) *Service {
	return NewService(repo, 3, 60*time.Second, func() time.Time { return fixedNow })
}

func TestRegisterTrimsInputAndPassesConfiguredLimit(t *testing.T) {
	repo := &stubRepo{}

	_, err := newTestService(repo).Register(context.Background(), "org-1", "  build-01 ", " linux ")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	want := NewDevice{OrgID: "org-1", Name: "build-01", Platform: "linux"}
	if repo.created != want {
		t.Errorf("created = %+v, want %+v", repo.created, want)
	}
	if repo.createdLimit != 3 {
		t.Errorf("limit passed to repo = %d, want 3", repo.createdLimit)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	tests := map[string]struct{ org, name, platform string }{
		"empty name":        {"org-1", "", "linux"},
		"blank name":        {"org-1", "   ", "linux"},
		"empty platform":    {"org-1", "a", ""},
		"empty org":         {"", "a", "linux"},
		"name too long":     {"org-1", strings.Repeat("a", maxNameLength+1), "linux"},
		"platform too long": {"org-1", "a", strings.Repeat("a", maxPlatformLength+1)},
		"nul in name":       {"org-1", "a\x00b", "linux"},
		"nul in platform":   {"org-1", "a", "li\x00nux"},
		"nul in org":        {"org\x00-1", "a", "linux"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			repo := &stubRepo{}

			_, err := newTestService(repo).Register(context.Background(), tc.org, tc.name, tc.platform)

			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want *InvalidInputError", err)
			}
			if repo.created != (NewDevice{}) {
				t.Error("repo must not be called for invalid input")
			}
		})
	}
}

func TestRegisterAcceptsMaxLengthName(t *testing.T) {
	name := strings.Repeat("a", maxNameLength)

	if _, err := newTestService(&stubRepo{}).Register(context.Background(), "org-1", name, "linux"); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRegisterReturnsRepositoryErrors(t *testing.T) {
	for _, repoErr := range []error{ErrNameTaken, ErrLimitReached} {
		_, err := newTestService(&stubRepo{err: repoErr}).Register(context.Background(), "org-1", "a", "linux")

		if !errors.Is(err, repoErr) {
			t.Errorf("err = %v, want %v", err, repoErr)
		}
	}
}

func TestRecordHeartbeatUsesServiceClock(t *testing.T) {
	repo := &stubRepo{}

	if err := newTestService(repo).RecordHeartbeat(context.Background(), "org-1", "id-1"); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	if !repo.heartbeatAt.Equal(fixedNow) {
		t.Errorf("heartbeat time = %v, want %v", repo.heartbeatAt, fixedNow)
	}
}

func TestRecordHeartbeatReturnsNotFound(t *testing.T) {
	err := newTestService(&stubRepo{err: ErrNotFound}).RecordHeartbeat(context.Background(), "org-1", "id-1")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkStaleOfflineCutoffIsTimeoutBeforeNow(t *testing.T) {
	repo := &stubRepo{staleAffected: 2}

	n, err := newTestService(repo).MarkStaleOffline(context.Background())
	if err != nil {
		t.Fatalf("MarkStaleOffline: %v", err)
	}

	if want := fixedNow.Add(-60 * time.Second); !repo.staleCutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", repo.staleCutoff, want)
	}
	if n != 2 {
		t.Errorf("affected = %d, want 2", n)
	}
}

func TestEveryOperationUsesTheSameNormalizedOrgID(t *testing.T) {
	ctx := context.Background()
	operations := map[string]func(*Service) error{
		"register":  func(s *Service) error { _, err := s.Register(ctx, "  acme ", "a", "linux"); return err },
		"list":      func(s *Service) error { _, err := s.List(ctx, "  acme "); return err },
		"get":       func(s *Service) error { _, err := s.Get(ctx, "  acme ", "id"); return err },
		"delete":    func(s *Service) error { return s.Delete(ctx, "  acme ", "id") },
		"heartbeat": func(s *Service) error { return s.RecordHeartbeat(ctx, "  acme ", "id") },
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			repo := &stubRepo{}

			if err := operation(newTestService(repo)); err != nil {
				t.Fatalf("operation: %v", err)
			}

			got := repo.org
			if name == "register" {
				got = repo.created.OrgID
			}
			if got != "acme" {
				t.Errorf("repo received org %q, want %q", got, "acme")
			}
		})
	}
}

func TestInvalidOrgIDIsRejectedByEveryOperation(t *testing.T) {
	ctx := context.Background()
	tooLong := strings.Repeat("a", maxOrgIDLength+1)
	operations := map[string]func(*Service) error{
		"list":      func(s *Service) error { _, err := s.List(ctx, tooLong); return err },
		"get":       func(s *Service) error { _, err := s.Get(ctx, "a\x00", "id"); return err },
		"delete":    func(s *Service) error { return s.Delete(ctx, "  ", "id") },
		"heartbeat": func(s *Service) error { return s.RecordHeartbeat(ctx, tooLong, "id") },
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			var invalid *InvalidInputError

			err := operation(newTestService(&stubRepo{}))

			if !errors.As(err, &invalid) {
				t.Errorf("err = %v, want *InvalidInputError", err)
			}
		})
	}
}
