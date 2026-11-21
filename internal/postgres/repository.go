package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"device-registry/internal/device"
)

const (
	pgInvalidTextRepresentation = "22P02"
	pgUniqueViolation           = "23505"

	deviceColumns = `id::text, org_id, name, platform, status, last_seen_at, created_at`
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, nd device.NewDevice, maxDevices int) (device.Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return device.Device{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, nd.OrgID); err != nil {
		return device.Device{}, fmt.Errorf("lock org: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM devices WHERE org_id = $1`, nd.OrgID).Scan(&count); err != nil {
		return device.Device{}, fmt.Errorf("count org devices: %w", err)
	}
	if count >= maxDevices {
		return device.Device{}, device.ErrLimitReached
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO devices (org_id, name, platform) VALUES ($1, $2, $3) RETURNING `+deviceColumns,
		nd.OrgID, nd.Name, nd.Platform)
	d, err := scanDevice(row)
	if err != nil {
		return device.Device{}, fmt.Errorf("insert device: %w", translate(err))
	}

	if err := tx.Commit(ctx); err != nil {
		return device.Device{}, fmt.Errorf("commit: %w", err)
	}
	return d, nil
}

func (r *Repository) List(ctx context.Context, orgID string) ([]device.Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE org_id = $1 ORDER BY created_at, id`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	devices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (device.Device, error) {
		return scanDevice(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

func (r *Repository) Get(ctx context.Context, orgID, id string) (device.Device, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE org_id = $1 AND id = $2::uuid`, orgID, id)
	d, err := scanDevice(row)
	if err != nil {
		return device.Device{}, fmt.Errorf("get device: %w", translate(err))
	}
	return d, nil
}

func (r *Repository) Delete(ctx context.Context, orgID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE org_id = $1 AND id = $2::uuid`, orgID, id)
	if err != nil {
		return fmt.Errorf("delete device: %w", translate(err))
	}
	if tag.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

func (r *Repository) RecordHeartbeat(ctx context.Context, orgID, id string, seenAt time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE devices SET status = 'online', last_seen_at = $3 WHERE org_id = $1 AND id = $2::uuid`,
		orgID, id, seenAt)
	if err != nil {
		return fmt.Errorf("record heartbeat: %w", translate(err))
	}
	if tag.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

func (r *Repository) MarkStaleOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE devices SET status = 'offline' WHERE status = 'online' AND last_seen_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("mark stale devices offline: %w", err)
	}
	return tag.RowsAffected(), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(row rowScanner) (device.Device, error) {
	var d device.Device
	err := row.Scan(&d.ID, &d.OrgID, &d.Name, &d.Platform, &d.Status, &d.LastSeenAt, &d.CreatedAt)
	return d, err
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return device.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgInvalidTextRepresentation:
			return device.ErrNotFound
		case pgUniqueViolation:
			return device.ErrNameTaken
		}
	}
	return err
}
