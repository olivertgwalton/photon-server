package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pipeline is a transaction that holds back each write, which answers nothing, and sends the writes
// held in order with the next statement that does answer, or at flush: a save of many writes and a
// few reads costs a round trip a read rather than one a statement, and each read still sees every
// write before it. A write that fails fails the next read or the flush, and with it the transaction.
type pipeline struct {
	tx   pgx.Tx
	held pgx.Batch
}

func (p *pipeline) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p.held.Queue(sql, args...)
	return pgconn.CommandTag{}, nil
}

func (p *pipeline) SendBatch(_ context.Context, b *pgx.Batch) pgx.BatchResults {
	p.held.QueuedQueries = append(p.held.QueuedQueries, b.QueuedQueries...)
	return heldBatch{}
}

func (p *pipeline) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if p.held.Len() == 0 {
		return p.tx.Query(ctx, sql, args...)
	}
	b := p.held
	p.held = pgx.Batch{}
	b.Queue(sql, args...)
	results := p.tx.SendBatch(ctx, &b)
	for range b.Len() - 1 {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return nil, err
		}
	}
	rows, err := results.Query()
	if err != nil {
		_ = results.Close()
		return nil, err
	}
	return batchRows{Rows: rows, results: results}, nil
}

func (p *pipeline) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	rows, err := p.Query(ctx, sql, args...)
	return firstRow{rows, err}
}

// flush sends the writes still held.
func (p *pipeline) flush(ctx context.Context) error {
	if p.held.Len() == 0 {
		return nil
	}
	b := p.held
	p.held = pgx.Batch{}
	return p.tx.SendBatch(ctx, &b).Close()
}

// heldBatch is the answer to a batch the pipeline holds: nothing yet, as for any write it holds.
type heldBatch struct{}

func (heldBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil }
func (heldBatch) Query() (pgx.Rows, error)         { return nil, pgx.ErrTxClosed }
func (heldBatch) QueryRow() pgx.Row                { return firstRow{err: pgx.ErrTxClosed} }
func (heldBatch) Close() error                     { return nil }

// batchRows are the rows of a batch's last statement, which close the batch with them.
type batchRows struct {
	pgx.Rows
	results pgx.BatchResults
}

func (r batchRows) Close() {
	r.Rows.Close()
	_ = r.results.Close()
}

// firstRow is QueryRow's answer over Query's: the first row, or pgx.ErrNoRows.
type firstRow struct {
	rows pgx.Rows
	err  error
}

func (r firstRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	if err := r.rows.Scan(dest...); err != nil {
		return err
	}
	r.rows.Close()
	return r.rows.Err()
}

// pipelined runs f in a transaction whose writes go to Postgres with its reads.
func (s *Store) pipelined(ctx context.Context, f func(tx db) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p := &pipeline{tx: tx}
		if err := f(p); err != nil {
			return err
		}
		return p.flush(ctx)
	})
}
