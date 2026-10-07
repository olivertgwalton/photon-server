package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const minimumPostgres = 180000

//go:embed migrations/*.sql
var migrations embed.FS

// Store reaches Postgres through pool; sql is the same pool as goose takes it.
type Store struct {
	pool *pgxpool.Pool
	sql  *sql.DB
}

// db is what a statement runs on: the pool, or a transaction begun on it.
type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
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
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(url, "pool_max_conns") {
		// As many connections as a node's job slots, conversions and background tasks may want (half
		// its CPUs for analysis, and two dozen more for scans, matches, webhooks, previews,
		// conversions and tasks), and as many again for requests. pgx's own default, one a CPU, let
		// a small machine's long scan or match queue every request behind it.
		cfg.MaxConns = int32(runtime.NumCPU() + 48)
	}
	cfg.ConnConfig.Tracer = queryLog{log}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool, sql: stdlib.OpenDBFromPool(pool)}
	if err := s.checkPostgres(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// slowQuery is how long a statement runs before it is logged.
const slowQuery = 200 * time.Millisecond

// queryLog logs each statement that fails, and each statement or batch that runs longer than
// slowQuery, without its values, which may be a provider's key.
type queryLog struct{ log *slog.Logger }

type queryStarted struct{}

type startedQuery struct {
	sql string
	at  time.Time
}

func (l queryLog) start(ctx context.Context, sql string) context.Context {
	return context.WithValue(ctx, queryStarted{}, startedQuery{sql, time.Now()})
}

func (l queryLog) end(ctx context.Context, err error) {
	q, _ := ctx.Value(queryStarted{}).(startedQuery)
	took := time.Since(q.at)
	switch {
	case err != nil:
		l.failed(ctx, q.sql, err)
	case took > slowQuery:
		l.log.WarnContext(ctx, "slow query", slog.Duration("took", took), slog.String("sql", q.sql))
	}
}

func (l queryLog) failed(ctx context.Context, sql string, err error) {
	l.log.WarnContext(ctx, "query failed", slog.String("sql", sql), slog.Any("err", err))
}

func (l queryLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, q pgx.TraceQueryStartData) context.Context {
	return l.start(ctx, q.SQL)
}

func (l queryLog) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, q pgx.TraceQueryEndData) {
	l.end(ctx, q.Err)
}

func (l queryLog) TraceBatchStart(ctx context.Context, _ *pgx.Conn, b pgx.TraceBatchStartData) context.Context {
	return l.start(ctx, fmt.Sprintf("batch of %d", b.Batch.Len()))
}

func (l queryLog) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, q pgx.TraceBatchQueryData) {
	if q.Err != nil {
		l.failed(ctx, q.SQL, q.Err)
	}
}

// TraceBatchEnd times the batch; TraceBatchQuery has logged the statement in it that failed.
func (l queryLog) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchEndData) {
	l.end(ctx, nil)
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
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, s.sql, dir)
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
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, "SELECT id FROM server").Scan(&id)
	return id, err
}

// SigningKey is the key the server signs stream addresses with, made once by its migration.
func (s *Store) SigningKey(ctx context.Context) ([]byte, error) {
	var key []byte
	err := s.pool.QueryRow(ctx, "SELECT signing_key FROM server").Scan(&key)
	return key, err
}

// SetCertificateCountry keeps the country providers fetch certificates in, by its ISO code, so a
// bare certificate is read in its system (India's A is for adults, Bulgaria's for anyone); "" for
// none.
func (s *Store) SetCertificateCountry(ctx context.Context, country string) error {
	_, err := s.pool.Exec(ctx, "UPDATE server SET certificate_country = nullif(upper($1), '')", country)
	return err
}

// Maintenance answers the maintenance window and when work that reads media waits for it.
func (s *Store) Maintenance(ctx context.Context) (domain.Maintenance, error) {
	var m domain.Maintenance
	var zone string
	err := s.pool.QueryRow(ctx, `
		SELECT maintenance_start, maintenance_end, maintenance_zone, previews_timing, markers_timing FROM server`).
		Scan(&m.StartHour, &m.EndHour, &zone, &m.Previews, &m.Markers)
	if err != nil {
		return m, err
	}
	m.Zone, err = domain.ParseZone(zone)
	return m, err
}

// SetMaintenance replaces the maintenance window and when work that reads media waits for it.
func (s *Store) SetMaintenance(ctx context.Context, m domain.Maintenance) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE server SET maintenance_start = $1, maintenance_end = $2, maintenance_zone = $3, previews_timing = $4,
			markers_timing = $5`, m.StartHour, m.EndHour, m.Zone.String(), m.Previews, m.Markers)
	return err
}

// Network answers whether the server's port answers HTTPS, the certificate it serves, and whether
// it answers Jellyfin's API, on which port.
func (s *Store) Network(ctx context.Context) (domain.Network, error) {
	var n domain.Network
	var cert, key *string
	err := s.pool.QueryRow(ctx, `SELECT secure_connections, tls_certificate, tls_key, jellyfin, jellyfin_port FROM server`).
		Scan(&n.Secure, &cert, &key, &n.Jellyfin, &n.JellyfinPort)
	n.Certificate, n.Key = deref(cert), deref(key)
	return n, err
}

func (s *Store) SetNetwork(ctx context.Context, n domain.Network) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE server SET secure_connections = $1, tls_certificate = $2, tls_key = $3, jellyfin = $4, jellyfin_port = $5`,
		n.Secure, optional(n.Certificate), optional(n.Key), n.Jellyfin, n.JellyfinPort)
	return err
}

// found turns a read that found no row into ErrNotFound.
func found(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// affected turns a write that touched no row into ErrNotFound.
func affected(tag pgconn.CommandTag, err error) error {
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// violation is the SQLSTATE Postgres refuses a write with for breaking a kind of constraint.
type violation string

const (
	uniqueViolation     violation = "23505"
	foreignKeyViolation violation = "23503"
)

// violates reports whether a write was refused for breaking a constraint of the kind.
func violates(err error, v violation) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && violation(pg.Code) == v
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Version answers the Postgres server's version.
func (s *Store) Version(ctx context.Context) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, "SHOW server_version").Scan(&v)
	return v, err
}
