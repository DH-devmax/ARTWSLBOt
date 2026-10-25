package database

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	Platform *pgxpool.Pool
	Tenant   *pgxpool.Pool
}

func Open(ctx context.Context, platformURL, tenantURL string) (*DB, error) {
	if platformURL == "" || tenantURL == "" || platformURL == tenantURL {
		return nil, fmt.Errorf("distinct platform and tenant database URLs are required")
	}
	p, err := pgxpool.ParseConfig(platformURL)
	if err != nil {
		return nil, fmt.Errorf("invalid platform database configuration")
	}
	p.MaxConns = 12
	platform, err := pgxpool.NewWithConfig(ctx, p)
	if err != nil {
		return nil, err
	}
	t, err := pgxpool.ParseConfig(tenantURL)
	if err != nil {
		platform.Close()
		return nil, fmt.Errorf("invalid tenant database configuration")
	}
	t.MaxConns = 8
	tenant, err := pgxpool.NewWithConfig(ctx, t)
	if err != nil {
		platform.Close()
		return nil, err
	}
	db := &DB{Platform: platform, Tenant: tenant}
	if err = platform.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("platform database unavailable: %w", err)
	}
	var role string
	var superuser bool
	if err = tenant.QueryRow(ctx, `SELECT current_user, rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&role, &superuser); err != nil {
		db.Close()
		return nil, err
	}
	if role != "wsl_tenant" || superuser {
		db.Close()
		return nil, fmt.Errorf("tenant pool must use non-superuser wsl_tenant")
	}
	if err = platform.QueryRow(ctx, `SELECT current_user, rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&role, &superuser); err != nil {
		db.Close()
		return nil, err
	}
	if role != "wsl_platform" || superuser {
		db.Close()
		return nil, fmt.Errorf("platform pool must use non-superuser wsl_platform")
	}
	return db, nil
}

func (d *DB) Close() {
	d.Platform.Close()
	d.Tenant.Close()
}

// TenantTx always sets the tenant inside the transaction, never on a pooled session.
func (d *DB) TenantTx(ctx context.Context, tenant string, fn func(pgx.Tx) error) error {
	tx, err := d.Tenant.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func Migrate(ctx context.Context, url string) error {
	if url == "" {
		return fmt.Errorf("MIGRATION_DATABASE_URL is required")
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(798421)`); err != nil {
		return err
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(798421)`)
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations
        (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, f.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrations.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, f.Name())
		}
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", f.Name(), err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
