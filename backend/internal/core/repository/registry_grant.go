package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/imageref"
	"github.com/google/uuid"
)

type RegistryGrantRepository struct {
	db     *sql.DB
	lockDB *sql.DB
}

func NewRegistryGrantRepository(db, lockDB *sql.DB) *RegistryGrantRepository {
	return &RegistryGrantRepository{db: db, lockDB: lockDB}
}

func (r *RegistryGrantRepository) ClaimBuild(ctx context.Context, id uuid.UUID, hash []byte, lifetime time.Duration) (model.Build, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Build{}, false, wrapOp("registry grant claim build", err)
	}
	defer tx.Rollback()
	var b model.Build
	var tag string
	err = tx.QueryRowContext(ctx, `SELECT b.id, b.image_id, b.owner_id, b.status, i.tag FROM builds b JOIN images i ON i.id=b.image_id AND i.owner_id=b.owner_id WHERE b.id=$1 FOR UPDATE OF b`, id).Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &tag)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, apperrors.ErrNotFound
	}
	if err != nil {
		return b, false, wrapOp("registry grant claim build", err)
	}
	if b.Status != model.BuildStatusPending {
		return b, false, nil
	}
	base, version := imageref.ParseTag(tag)
	repository := imageref.CustomRepositoryName(b.OwnerID, base)
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 84627))`, repository).Scan(&locked); err != nil {
		return b, false, wrapOp("registry grant claim build", err)
	}
	if !locked {
		return b, false, apperrors.ErrUnavailable
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO registry_build_credentials(build_id,owner_id,repository,tag,secret_hash,expires_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()+$6*interval '1 second')`, b.ID, b.OwnerID, repository, version, hash, lifetime.Seconds())
	if err != nil {
		return b, false, wrapOp("registry grant claim build", err)
	}
	err = tx.QueryRowContext(ctx, `UPDATE builds SET status='running', started_at=clock_timestamp(), finished_at=NULL WHERE id=$1 RETURNING created_at, started_at`, id).Scan(&b.CreatedAt, &b.StartedAt)
	if err != nil {
		return b, false, wrapOp("registry grant claim build", err)
	}
	if err := commitTx(tx, "claim build"); err != nil {
		return b, false, err
	}
	b.Status = model.BuildStatusRunning
	b.RegistryCredential.Destination = repository + ":" + version
	return b, true, nil
}

func (r *RegistryGrantRepository) AuthorizeCredential(ctx context.Context, hash []byte, now time.Time) (model.RegistryPrincipal, error) {
	var p model.RegistryPrincipal
	err := r.db.QueryRowContext(ctx, `UPDATE registry_build_credentials c SET last_issued_at=GREATEST(c.last_issued_at,$2)
 FROM builds b JOIN images i ON i.id=b.image_id AND i.owner_id=b.owner_id
 WHERE c.secret_hash=$1 AND c.build_id=b.id AND c.owner_id=b.owner_id AND b.status='running' AND i.status='building'
 AND c.revoked_at IS NULL AND c.expires_at>$2
 RETURNING c.build_id,c.owner_id,c.repository`, hash, now).Scan(&p.BuildID, &p.OwnerID, &p.Repository)
	if errors.Is(err, sql.ErrNoRows) {
		err = r.db.QueryRowContext(ctx, `SELECT c.id,c.owner_id,c.repository FROM registry_pull_credentials c WHERE c.secret_hash=$1 AND c.expires_at>$2 AND EXISTS(SELECT 1 FROM images i WHERE i.owner_id=c.owner_id AND i.status='available' AND lower(i.owner_id::text || '_' || split_part(i.tag,':',1))=c.repository)`, hash, now).Scan(&p.BuildID, &p.OwnerID, &p.Repository)
		if errors.Is(err, sql.ErrNoRows) {
			return p, apperrors.ErrForbidden
		}
		p.ReadOnly = true
	}
	if err != nil {
		return p, apperrors.ErrUnavailable
	}
	if p.ReadOnly {
		return p, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT lower(owner_id::text || '_' || split_part(tag,':',1)) FROM images WHERE owner_id=$1 AND status='available'`, p.OwnerID)
	if err != nil {
		return p, apperrors.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return p, apperrors.ErrUnavailable
		}
		p.PullRepositories = append(p.PullRepositories, name)
	}
	if err := rows.Err(); err != nil {
		return p, apperrors.ErrUnavailable
	}
	return p, nil
}

func (r *RegistryGrantRepository) RepositoryExists(ctx context.Context, repository string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM images WHERE lower(owner_id::text || '_' || split_part(tag,':',1))=$1 UNION ALL SELECT 1 FROM registry_cleanup_jobs WHERE repository=$1)`, repository).Scan(&exists)
	if err != nil {
		return false, apperrors.ErrUnavailable
	}
	return exists, nil
}

func (r *RegistryGrantRepository) RevokeBuild(ctx context.Context, id uuid.UUID, cleanup bool, fence time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapOp("registry grant revoke build", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE registry_build_credentials SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE build_id=$1`, id)
	if err != nil {
		return wrapOp("registry grant revoke build", err)
	}
	if cleanup {
		_, err = tx.ExecContext(ctx, `INSERT INTO registry_cleanup_jobs(id,owner_id,repository,tag,not_before)
   SELECT build_id,owner_id,repository,tag,last_issued_at+$2*interval '1 second' FROM registry_build_credentials WHERE build_id=$1
   ON CONFLICT(id) DO UPDATE SET not_before=GREATEST(registry_cleanup_jobs.not_before,EXCLUDED.not_before)`, id, fence.Seconds())
		if err != nil {
			return wrapOp("registry grant revoke build", err)
		}
	}
	return commitTx(tx, "revoke build")
}

// FenceRepository revokes the repository's build credentials whose builds no
// longer run and returns how long their issued tokens may still write. A build
// claimed concurrently with an unlocked fence keeps its credential; the caller
// detects it as active. The database clock measures the remainder so Core clock
// skew cannot shorten it.
func (r *RegistryGrantRepository) FenceRepository(ctx context.Context, repository string, fence time.Duration) (time.Duration, error) {
	var seconds float64
	err := r.db.QueryRowContext(ctx, `WITH revoked AS (
 UPDATE registry_build_credentials c SET revoked_at=COALESCE(c.revoked_at,clock_timestamp())
 WHERE c.repository=$1 AND NOT EXISTS(SELECT 1 FROM builds b WHERE b.id=c.build_id AND b.status='running')
 RETURNING c.last_issued_at)
 SELECT COALESCE(EXTRACT(EPOCH FROM max(last_issued_at)+$2*interval '1 second'-clock_timestamp()),0)::float8 FROM revoked`, repository, fence.Seconds()).Scan(&seconds)
	if err != nil {
		return 0, wrapOp("registry grant fence repository", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func (r *RegistryGrantRepository) PendingRegistryCleanup(ctx context.Context) ([]model.RegistryCleanup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,repository,tag FROM registry_cleanup_jobs WHERE not_before<clock_timestamp() ORDER BY not_before LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.RegistryCleanup
	for rows.Next() {
		var job model.RegistryCleanup
		if err := rows.Scan(&job.ID, &job.Repository, &job.Tag); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *RegistryGrantRepository) CompleteRegistryCleanup(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM registry_cleanup_jobs WHERE id=$1`, id)
	return err
}

func (r *RegistryGrantRepository) SavePullCredential(ctx context.Context, id uuid.UUID, repository string, hash []byte, lifetime time.Duration) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM registry_pull_credentials WHERE expires_at<clock_timestamp()`)
	if err != nil {
		return apperrors.ErrUnavailable
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO registry_pull_credentials(id,owner_id,repository,secret_hash,expires_at)
 SELECT $1,owner_id,$2,$3,clock_timestamp()+$4*interval '1 second' FROM images
 WHERE status='available' AND lower(owner_id::text || '_' || split_part(tag,':',1))=$2 LIMIT 1`, id, repository, hash, lifetime.Seconds())
	if err != nil {
		return apperrors.ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return apperrors.ErrUnavailable
	}
	if count != 1 {
		return apperrors.ErrForbidden
	}
	return nil
}

// Claims take the same transaction lock without waiting. Cleanup holds it until
// the Registry operation finishes, so a new publisher cannot race deletion.
func (r *RegistryGrantRepository) LockRegistryRepository(ctx context.Context, repository string) (func(), error) {
	tx, err := r.lockDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 84627))`, repository); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return func() { _ = tx.Rollback() }, nil
}

func (r *RegistryGrantRepository) RegistryCleanupState(ctx context.Context, job model.RegistryCleanup, fence time.Duration) (model.RegistryCleanupState, error) {
	var state model.RegistryCleanupState
	err := r.db.QueryRowContext(ctx, `SELECT CASE
 WHEN EXISTS(SELECT 1 FROM builds b JOIN images i ON i.id=b.image_id
   WHERE b.status IN ('pending','running') AND lower(i.owner_id::text || '_' || split_part(i.tag,':',1))=$1 )
 OR EXISTS(SELECT 1 FROM registry_build_credentials WHERE repository=$1 AND last_issued_at+$3*interval '1 second'>clock_timestamp())
 THEN 'pending'
 WHEN EXISTS(SELECT 1 FROM images WHERE status='available' AND lower(owner_id::text || '_' || split_part(tag,':',1))=$1 AND split_part(tag,':',2)=$2)
 THEN 'obsolete' ELSE 'ready' END`, job.Repository, job.Tag, fence.Seconds()).Scan(&state)
	return state, err
}

func (r *RegistryGrantRepository) HasActiveRegistryBuilds(ctx context.Context, repository string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM registry_build_credentials c JOIN builds b ON b.id=c.build_id WHERE c.repository=$1 AND b.status='running')`, repository).Scan(&active)
	return active, err
}
