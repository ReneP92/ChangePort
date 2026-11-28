package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	sf "github.com/snowflakedb/gosnowflake"
)

// The spike of how a batch lands (#219, docs/snowflake.md): a batch of a
// table's changes goes into a temporary table, then one request MERGEs it
// into the warehouse table, keeping the latest change per key, and records
// the offsets it consumed, in one transaction.

// change is one change of a row, as the sink reads it from a table's topic.
type change struct {
	id      int64
	name    string
	amount  string
	offset  int64 // its offset in the table's partition
	deleted bool
}

const (
	createTarget = `CREATE TABLE ORDERS (
		ID NUMBER(38,0) NOT NULL PRIMARY KEY,
		NAME TEXT,
		AMOUNT NUMBER(18,2),
		_CHANGEPORT_OFFSET NUMBER(38,0) NOT NULL,
		_CHANGEPORT_DELETED BOOLEAN NOT NULL,
		_CHANGEPORT_SYNCED_AT TIMESTAMP_TZ NOT NULL)`
	createOffsets = `CREATE TABLE _CHANGEPORT_OFFSETS (
		TOPIC TEXT NOT NULL, PARTITION NUMBER(10,0) NOT NULL, NEXT_OFFSET NUMBER(38,0) NOT NULL,
		PRIMARY KEY (TOPIC, PARTITION))`
	createBatch = `CREATE OR REPLACE TEMPORARY TABLE ORDERS_BATCH (
		ID NUMBER(38,0), NAME TEXT, AMOUNT NUMBER(18,2), _CHANGEPORT_OFFSET NUMBER(38,0), _CHANGEPORT_DELETED BOOLEAN)`
	// mergeBatch keeps each key's latest change in the batch and applies it
	// unless the row already holds a later one, so applying a batch twice
	// changes nothing. A delete keeps the row, marked, so that a change
	// older than the delete cannot bring the row back, and with its last
	// values, as a delete carries only the key.
	mergeBatch = `MERGE INTO ORDERS T USING (
			SELECT * FROM ORDERS_BATCH QUALIFY ROW_NUMBER() OVER (PARTITION BY ID ORDER BY _CHANGEPORT_OFFSET DESC) = 1
		) S ON T.ID = S.ID
		WHEN MATCHED AND S._CHANGEPORT_OFFSET > T._CHANGEPORT_OFFSET THEN UPDATE SET
			NAME = IFF(S._CHANGEPORT_DELETED, T.NAME, S.NAME), AMOUNT = IFF(S._CHANGEPORT_DELETED, T.AMOUNT, S.AMOUNT),
			_CHANGEPORT_OFFSET = S._CHANGEPORT_OFFSET,
			_CHANGEPORT_DELETED = S._CHANGEPORT_DELETED, _CHANGEPORT_SYNCED_AT = CURRENT_TIMESTAMP()
		WHEN NOT MATCHED THEN INSERT (ID, NAME, AMOUNT, _CHANGEPORT_OFFSET, _CHANGEPORT_DELETED, _CHANGEPORT_SYNCED_AT)
			VALUES (S.ID, S.NAME, S.AMOUNT, S._CHANGEPORT_OFFSET, S._CHANGEPORT_DELETED, CURRENT_TIMESTAMP())`
	// mergeOffset records the next offset, never an earlier one: a sink
	// thought dead but still running, on a node cut off from the cluster,
	// cannot move it back.
	mergeOffset = `MERGE INTO _CHANGEPORT_OFFSETS T USING (SELECT ? AS TOPIC, 0 AS PARTITION, ? AS NEXT_OFFSET) S
		ON T.TOPIC = S.TOPIC AND T.PARTITION = S.PARTITION
		WHEN MATCHED AND S.NEXT_OFFSET > T.NEXT_OFFSET THEN UPDATE SET NEXT_OFFSET = S.NEXT_OFFSET
		WHEN NOT MATCHED THEN INSERT (TOPIC, PARTITION, NEXT_OFFSET) VALUES (S.TOPIC, S.PARTITION, S.NEXT_OFFSET)`
	topic = "demo.public.orders"
)

// setUpTables creates the warehouse table and the offsets table.
func setUpTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{createTarget, createOffsets} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

// loadBatch puts a batch into the session's temporary batch table with
// bound arrays.
func loadBatch(ctx context.Context, conn *sql.Conn, batch []change) error {
	if _, err := conn.ExecContext(ctx, createBatch); err != nil {
		return err
	}
	ids, names, amounts, offsets, deleted := make([]int64, len(batch)), make([]string, len(batch)), make([]string, len(batch)), make([]int64, len(batch)), make([]bool, len(batch))
	for i, c := range batch {
		ids[i], names[i], amounts[i], offsets[i], deleted[i] = c.id, c.name, c.amount, c.offset, c.deleted
	}
	_, err := conn.ExecContext(ctx, "INSERT INTO ORDERS_BATCH VALUES (?, ?, ?, ?, ?)",
		sf.Array(&ids), sf.Array(&names), sf.Array(&amounts), sf.Array(&offsets), sf.Array(&deleted))
	return err
}

// commitBatch MERGEs the loaded batch and records the next offset in one
// request and one transaction, so that the server finishes it even if the
// sink is gone meanwhile. A statement that fails leaves the transaction open
// in the session, holding its locks, so it is rolled back.
func commitBatch(ctx context.Context, conn *sql.Conn, next int64) error {
	multi, err := sf.WithMultiStatement(ctx, 4)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(multi, "BEGIN; "+mergeBatch+"; "+mergeOffset+"; COMMIT;", topic, next); err != nil {
		if _, rollback := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rollback != nil {
			return errors.Join(err, fmt.Errorf("roll back: %w", rollback))
		}
		return err
	}
	return nil
}

// applyBatch loads a batch and commits it.
func applyBatch(t *testing.T, db *sql.DB, batch []change) {
	t.Helper()
	ctx := t.Context()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := loadBatch(ctx, conn, batch); err != nil {
		t.Fatalf("load the batch: %v", err)
	}
	if err := commitBatch(ctx, conn, batch[len(batch)-1].offset+1); err != nil {
		t.Fatalf("commit the batch: %v", err)
	}
}

// rowsOf returns the warehouse table, one line per row, by ID.
func rowsOf(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT ID, NAME, AMOUNT, _CHANGEPORT_OFFSET, _CHANGEPORT_DELETED FROM ORDERS ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var (
			id, offset int64
			name       sql.NullString
			amount     string
			deleted    bool
		)
		if err := rows.Scan(&id, &name, &amount, &offset, &deleted); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%d %s %s @%d deleted=%t", id, name.String, amount, offset, deleted))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// syncedAt returns when the warehouse table last changed, by its rows'
// _CHANGEPORT_SYNCED_AT.
func syncedAt(t *testing.T, db *sql.DB) string {
	t.Helper()
	var at string
	if err := db.QueryRowContext(t.Context(), "SELECT TO_VARCHAR(MAX(_CHANGEPORT_SYNCED_AT)) FROM ORDERS").Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

// nextOffset returns the offset the sink resumes from.
func nextOffset(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var next sql.NullInt64
	err := db.QueryRowContext(t.Context(), "SELECT MAX(NEXT_OFFSET) FROM _CHANGEPORT_OFFSETS WHERE TOPIC = ?", topic).Scan(&next)
	if err != nil {
		t.Fatal(err)
	}
	return next.Int64
}

func TestMergeKeepsEachKeysLatestChangeOnce(t *testing.T) {
	db, _ := testAccount(t)
	setUpTables(t, db)

	first := []change{
		{id: 1, name: "ada", amount: "10.00", offset: 0},
		{id: 2, name: "bob", amount: "20.00", offset: 1},
		{id: 1, name: "ada", amount: "11.50", offset: 2}, // the same key again in one batch
		{id: 3, name: "cy", amount: "30.00", offset: 3},
	}
	applyBatch(t, db, first)
	want := "1 ada 11.50 @2 deleted=false\n2 bob 20.00 @1 deleted=false\n3 cy 30.00 @3 deleted=false"
	if got := rowsOf(t, db); got != want {
		t.Fatalf("after the first batch:\n%s\nwant\n%s", got, want)
	}
	if got := nextOffset(t, db); got != 4 {
		t.Fatalf("next offset %d, want 4", got)
	}

	// A batch applied twice, as after a crash between merging and the sink
	// learning of it, changes nothing: no row is even written again.
	synced := syncedAt(t, db)
	applyBatch(t, db, first)
	if got := rowsOf(t, db); got != want {
		t.Fatalf("after the first batch again:\n%s\nwant\n%s", got, want)
	}
	if got := syncedAt(t, db); got != synced {
		t.Fatalf("the batch applied again wrote rows, at %s", got)
	}
	// Nor does an earlier batch move the offset back.
	applyBatch(t, db, first[:2])
	if got := nextOffset(t, db); got != 4 {
		t.Fatalf("next offset %d after an earlier batch, want 4", got)
	}

	// A delete, which carries only the key, keeps the row with its last
	// values, marked; a change older than it, such as a snapshot row read
	// again, cannot bring the row back.
	applyBatch(t, db, []change{{id: 2, offset: 4, amount: "0", deleted: true}})
	applyBatch(t, db, []change{{id: 2, name: "bobby", amount: "21.00", offset: 1}})
	want = "1 ada 11.50 @2 deleted=false\n2 bob 20.00 @4 deleted=true\n3 cy 30.00 @3 deleted=false"
	if got := rowsOf(t, db); got != want {
		t.Fatalf("after the delete and an older change:\n%s\nwant\n%s", got, want)
	}
}
