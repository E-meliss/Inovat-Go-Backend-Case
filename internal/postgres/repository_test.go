package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"device-registry/internal/device"
)

const highLimit = 1000

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE devices`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewRepository(pool)
}

func create(t *testing.T, repo *Repository, org, name string) device.Device {
	t.Helper()

	d, err := repo.Create(context.Background(), device.NewDevice{OrgID: org, Name: name, Platform: "linux"}, highLimit)
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return d
}

func TestCreateStartsOfflineAndCanBeFetched(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	created := create(t, repo, "org-1", "build-01")
	got, err := repo.Get(ctx, "org-1", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != device.StatusOffline || got.LastSeenAt != nil {
		t.Errorf("new device = status %q, last seen %v; want offline and never seen", got.Status, got.LastSeenAt)
	}
	if got.Name != "build-01" || got.Platform != "linux" || got.OrgID != "org-1" {
		t.Errorf("got %+v", got)
	}
}

func TestCreateRejectsDuplicateNameWithinOrgOnly(t *testing.T) {
	repo := newTestRepository(t)
	create(t, repo, "org-1", "build-01")

	_, err := repo.Create(context.Background(), device.NewDevice{OrgID: "org-1", Name: "build-01", Platform: "linux"}, highLimit)
	if !errors.Is(err, device.ErrNameTaken) {
		t.Errorf("same org: err = %v, want ErrNameTaken", err)
	}

	create(t, repo, "org-2", "build-01")
}

func TestCreateEnforcesLimitPerOrg(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	const limit = 2

	for i := range limit {
		nd := device.NewDevice{OrgID: "org-1", Name: fmt.Sprintf("d-%d", i), Platform: "linux"}
		if _, err := repo.Create(ctx, nd, limit); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	_, err := repo.Create(ctx, device.NewDevice{OrgID: "org-1", Name: "one-too-many", Platform: "linux"}, limit)
	if !errors.Is(err, device.ErrLimitReached) {
		t.Errorf("err = %v, want ErrLimitReached", err)
	}

	if _, err := repo.Create(ctx, device.NewDevice{OrgID: "org-2", Name: "fresh", Platform: "linux"}, limit); err != nil {
		t.Errorf("other org must have its own limit: %v", err)
	}
}

func TestCreateAfterDeleteFreesLimitSlot(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	first, err := repo.Create(ctx, device.NewDevice{OrgID: "org-1", Name: "a", Platform: "linux"}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Delete(ctx, "org-1", first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := repo.Create(ctx, device.NewDevice{OrgID: "org-1", Name: "b", Platform: "linux"}, 1); err != nil {
		t.Errorf("create after delete: %v", err)
	}
}

func TestConcurrentCreatesNeverExceedLimit(t *testing.T) {
	repo := newTestRepository(t)
	const (
		limit    = 5
		attempts = 20
	)

	errs := runConcurrently(attempts, func(i int) error {
		nd := device.NewDevice{OrgID: "org-1", Name: fmt.Sprintf("d-%d", i), Platform: "linux"}
		_, err := repo.Create(context.Background(), nd, limit)
		return err
	})

	succeeded, limited := countOutcomes(errs, device.ErrLimitReached)
	if succeeded != limit || limited != attempts-limit {
		t.Errorf("succeeded = %d, limited = %d; want %d and %d (errors: %v)", succeeded, limited, limit, attempts-limit, errs)
	}
}

func TestConcurrentCreatesWithSameNameAllowOnlyOne(t *testing.T) {
	repo := newTestRepository(t)
	const attempts = 10

	errs := runConcurrently(attempts, func(int) error {
		_, err := repo.Create(context.Background(), device.NewDevice{OrgID: "org-1", Name: "same", Platform: "linux"}, highLimit)
		return err
	})

	succeeded, taken := countOutcomes(errs, device.ErrNameTaken)
	if succeeded != 1 || taken != attempts-1 {
		t.Errorf("succeeded = %d, name taken = %d; want 1 and %d (errors: %v)", succeeded, taken, attempts-1, errs)
	}
}

func TestListReturnsOnlyOrgDevicesInCreationOrder(t *testing.T) {
	repo := newTestRepository(t)
	create(t, repo, "org-1", "first")
	create(t, repo, "org-2", "other-org")
	create(t, repo, "org-1", "second")

	got, err := repo.List(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Errorf("got %+v, want [first second]", got)
	}
}

func TestListOfUnknownOrgIsEmptyNotNil(t *testing.T) {
	repo := newTestRepository(t)

	got, err := repo.List(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got == nil || len(got) != 0 {
		t.Errorf("got %#v, want empty non-nil slice", got)
	}
}

func TestDeleteRemovesDeviceOnlyInItsOwnOrg(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	d := create(t, repo, "org-1", "a")

	if err := repo.Delete(ctx, "org-2", d.ID); !errors.Is(err, device.ErrNotFound) {
		t.Errorf("wrong org: err = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, "org-1", d.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
	if err := repo.Delete(ctx, "org-1", d.ID); !errors.Is(err, device.ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestUnknownOrMalformedIDIsNotFound(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	const unknownUUID = "00000000-0000-0000-0000-000000000000"

	for _, id := range []string{unknownUUID, "not-a-uuid"} {
		if _, err := repo.Get(ctx, "org-1", id); !errors.Is(err, device.ErrNotFound) {
			t.Errorf("Get(%q): err = %v, want ErrNotFound", id, err)
		}
		if err := repo.RecordHeartbeat(ctx, "org-1", id, time.Now()); !errors.Is(err, device.ErrNotFound) {
			t.Errorf("RecordHeartbeat(%q): err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestRecordHeartbeatMarksDeviceOnline(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	d := create(t, repo, "org-1", "a")
	seenAt := time.Now().UTC().Truncate(time.Microsecond)

	if err := repo.RecordHeartbeat(ctx, "org-1", d.ID, seenAt); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	got, err := repo.Get(ctx, "org-1", d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != device.StatusOnline || got.LastSeenAt == nil || !got.LastSeenAt.Equal(seenAt) {
		t.Errorf("got status %q, last seen %v; want online at %v", got.Status, got.LastSeenAt, seenAt)
	}
}

func TestRecordHeartbeatOfOtherOrgIsNotFound(t *testing.T) {
	repo := newTestRepository(t)
	d := create(t, repo, "org-1", "a")

	err := repo.RecordHeartbeat(context.Background(), "org-2", d.ID, time.Now())

	if !errors.Is(err, device.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkStaleOfflineOnlyTouchesDevicesSeenBeforeCutoff(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	seenAt := time.Now().UTC().Truncate(time.Microsecond)
	heartbeating := create(t, repo, "org-1", "heartbeating")
	create(t, repo, "org-1", "never-seen")
	if err := repo.RecordHeartbeat(ctx, "org-1", heartbeating.ID, seenAt); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	n, err := repo.MarkStaleOffline(ctx, seenAt)
	if err != nil {
		t.Fatalf("MarkStaleOffline: %v", err)
	}
	if n != 0 {
		t.Errorf("cutoff equal to last seen: affected = %d, want 0", n)
	}

	n, err = repo.MarkStaleOffline(ctx, seenAt.Add(time.Microsecond))
	if err != nil {
		t.Fatalf("MarkStaleOffline: %v", err)
	}
	if n != 1 {
		t.Errorf("cutoff after last seen: affected = %d, want 1", n)
	}

	got, err := repo.Get(ctx, "org-1", heartbeating.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != device.StatusOffline {
		t.Errorf("status = %q, want offline", got.Status)
	}
}

func runConcurrently(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = fn(i)
		}()
	}
	wg.Wait()
	return errs
}

func countOutcomes(errs []error, expected error) (succeeded, rejected int) {
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, expected):
			rejected++
		}
	}
	return succeeded, rejected
}
