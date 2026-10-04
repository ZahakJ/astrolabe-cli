---
tags: [concept, queues]
---
# Idempotency keys

An operation is idempotent when doing it twice has the same effect as doing
it once. With at-least-once delivery, every consumer will eventually see a
message twice, so every consumer must be idempotent or pretend well.

## The pattern

The producer attaches a key that names the *intent* (not the attempt). The
consumer records keys it has applied, inside the same transaction as the
effect:

```go
// Apply runs fn at most once per key. The key is stored in the same
// transaction as fn's writes, so a crash cannot apply without recording.
func Apply(ctx context.Context, db *sql.DB, key string, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO applied_keys (key, applied_at) VALUES ($1, now())
		 ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already applied: a duplicate delivery
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
```

Keys expire after the longest possible redelivery window (seven days for
Lantern), so the table stays small.

## Ordering is a separate problem

Idempotency makes *repeats* harmless; it says nothing about *order*. If
"set address to B" can arrive before "set address to A", dedupe will
happily apply both — in the wrong order.

The fix is a version on the entity:

- each update carries the version it was based on;
- the consumer applies it only if `version > stored_version`;
- older updates are acknowledged and dropped.

This is what billing needs before its [[Lantern cutover]], and what
[[Tidewater]] does with documents.

## Pitfalls

- Generating the key in the consumer — then every redelivery gets a new
  key and nothing is deduplicated.
- Storing the key *after* the side effect, outside the transaction.
- Using a timestamp as the key. Clocks skew; see
  [[2026-10-02 Platform sync#Lantern]].
