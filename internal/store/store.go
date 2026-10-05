package store

//go:generate go run ./gen

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/olivertgwalton/photon-server/internal/store/query"
)

const minimumPostgres = 180000

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	pool *pgxpool.Pool
	sql  *sql.DB
	q    *query.Query
}

// Open connects and refuses a database whose schema is not the one this binary was built for.
func Open(ctx context.Context, url string, log *slog.Logger) (*Store, error) {
	s, err := connect(ctx, url, log)
	if err != nil {
		return nil, err
	}
	if err := s.checkSchema(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func Migrate(ctx context.Context, url string, log *slog.Logger) error {
	s, err := connect(ctx, url, log)
	if err != nil {
		return err
	}
	defer s.Close()
	p, err := s.migrator()
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	for _, r := range results {
		log.InfoContext(ctx, "migrated", slog.String("migration", r.Source.Path), slog.Duration("took", r.Duration))
	}
	return err
}

func connect(ctx context.Context, url string, log *slog.Logger) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool, sql: stdlib.OpenDBFromPool(pool)}
	if err := s.checkPostgres(ctx); err != nil {
		s.Close()
		return nil, err
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: s.sql}), &gorm.Config{
		SkipDefaultTransaction: true,
		TranslateError:         true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
		Logger: logger.NewSlogLogger(log, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		s.Close()
		return nil, err
	}
	s.q = query.Use(db)
	return s, nil
}

func (s *Store) Close() {
	_ = s.sql.Close()
	s.pool.Close()
}

func (s *Store) checkPostgres(ctx context.Context) error {
	var num string
	if err := s.pool.QueryRow(ctx, "SHOW server_version_num").Scan(&num); err != nil {
		return err
	}
	if v, err := strconv.Atoi(num); err != nil || v < minimumPostgres {
		return fmt.Errorf("postgres %s is older than 18", num)
	}
	return nil
}

func (s *Store) migrator() (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectPostgres, s.sql, migrationsDir())
}

var errSchema = errors.New("schema version mismatch")

func (s *Store) checkSchema(ctx context.Context) error {
	p, err := s.migrator()
	if err != nil {
		return err
	}
	sources := p.ListSources()
	want := sources[len(sources)-1].Version
	have, err := p.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if have != want {
		return fmt.Errorf("%w: database is at %d, this binary needs %d (run photon-server migrate)", errSchema, have, want)
	}
	return nil
}

// ServerID is this installation's identity, minted once by the first migration.
func (s *Store) ServerID(ctx context.Context) (uuid.UUID, error) {
	row, err := s.q.Server.WithContext(ctx).Take()
	if err != nil {
		return uuid.UUID{}, err
	}
	return uuid.UUID(row.ID), nil
}

func migrationsDir() fs.FS {
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return dir
}
