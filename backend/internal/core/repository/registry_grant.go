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

type RegistryGrantRepository struct{ db *sql.DB }

func NewRegistryGrantRepository(db *sql.DB) *RegistryGrantRepository {
	return &RegistryGrantRepository{db: db}
}

func (r *RegistryGrantRepository) ClaimBuild(ctx context.Context, id uuid.UUID, hash []byte, lifetime time.Duration) (model.Build, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Build{}, false, err
	}
	defer tx.Rollback()
	var b model.Build
	var tag string
	err = tx.QueryRowContext(ctx, `SELECT b.id, b.image_id, b.owner_id, b.status, i.tag FROM builds b JOIN images i ON i.id=b.image_id AND i.owner_id=b.owner_id WHERE b.id=$1 FOR UPDATE OF b`, id).Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &tag)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, apperrors.ErrNotFound
	}
	if err != nil {
		return b, false, err
	}
	if b.Status != model.BuildStatusPending {
		return b, false, nil
	}
	base, version := imageref.ParseTag(tag)
	repository := imageref.CustomRepositoryName(b.OwnerID, base)
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 84627))`, repository).Scan(&locked); err != nil {
		return b, false, err
	}
	if !locked {
		return b, false, apperrors.ErrUnavailable
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO registry_build_credentials(build_id,owner_id,repository,tag,secret_hash,expires_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()+$6*interval '1 second')`, b.ID, b.OwnerID, repository, version, hash, lifetime.Seconds())
	if err != nil {
		return b, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE builds SET status='running', finished_at=NULL WHERE id=$1`, id)
	if err != nil {
		return b, false, err
	}
	if err := tx.Commit(); err != nil {
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

func (r *RegistryGrantRepository) RevokeBuild(ctx context.Context, id uuid.UUID, cleanup bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE registry_build_credentials SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE build_id=$1`, id)
	if err != nil {
		return err
	}
	if cleanup {
		_, err = tx.ExecContext(ctx, `INSERT INTO registry_cleanup_jobs(id,owner_id,repository,tag,not_before)
   SELECT build_id,owner_id,repository,tag,last_issued_at+interval '181 seconds' FROM registry_build_credentials WHERE build_id=$1
   ON CONFLICT(id) DO UPDATE SET not_before=GREATEST(registry_cleanup_jobs.not_before,EXCLUDED.not_before)`, id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *RegistryGrantRepository) FenceRepository(ctx context.Context, repository string) (time.Time, error) {
	var until time.Time
	err := r.db.QueryRowContext(ctx, `WITH revoked AS (
 UPDATE registry_build_credentials SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE repository=$1 RETURNING last_issued_at)
 SELECT COALESCE(max(last_issued_at)+interval '181 seconds',clock_timestamp()) FROM revoked`, repository).Scan(&until)
	return until, err
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

func (r *RegistryGrantRepository) SavePullCredential(ctx context.Context, id uuid.UUID, repository string, hash []byte) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM registry_pull_credentials WHERE expires_at<clock_timestamp()`)
	if err != nil {
		return apperrors.ErrUnavailable
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO registry_pull_credentials(id,owner_id,repository,secret_hash,expires_at)
 SELECT $1,owner_id,$2,$3,clock_timestamp()+interval '30 minutes' FROM images
 WHERE status='available' AND lower(owner_id::text || '_' || split_part(tag,':',1))=$2 LIMIT 1`, id, repository, hash)
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 84627))`, repository); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return func() { _ = tx.Rollback() }, nil
}

func (r *RegistryGrantRepository) RegistryCleanupState(ctx context.Context, job model.RegistryCleanup) (model.RegistryCleanupState, error) {
	var state model.RegistryCleanupState
	err := r.db.QueryRowContext(ctx, `SELECT CASE
 WHEN EXISTS(SELECT 1 FROM builds b JOIN images i ON i.id=b.image_id
   WHERE b.status IN ('pending','running') AND lower(i.owner_id::text || '_' || split_part(i.tag,':',1))=$1 )
 OR EXISTS(SELECT 1 FROM registry_build_credentials WHERE repository=$1 AND last_issued_at+interval '181 seconds'>clock_timestamp())
 THEN 'pending'
 WHEN EXISTS(SELECT 1 FROM images WHERE status='available' AND lower(owner_id::text || '_' || split_part(tag,':',1))=$1 AND split_part(tag,':',2)=$2)
 THEN 'obsolete' ELSE 'ready' END`, job.Repository, job.Tag).Scan(&state)
	return state, err
}

func (r *RegistryGrantRepository) HasActiveRegistryBuilds(ctx context.Context, repository string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM registry_build_credentials c JOIN builds b ON b.id=c.build_id WHERE c.repository=$1 AND b.status='running')`, repository).Scan(&active)
	return active, err
}
