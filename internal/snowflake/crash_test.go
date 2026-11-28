package snowflake

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	sf "github.com/snowflakedb/gosnowflake"
)

// The spike of a sink that dies mid-batch (#219, docs/snowflake.md): the test
// runs this binary again as a sink, kills it at the worst moment with
// SIGKILL, and looks at what the warehouse holds and whether the dead sink's
// transaction still holds its locks.

const (
	childEnv       = "SNOWFLAKE_SPIKE_CHILD"
	childSchemaEnv = "SNOWFLAKE_SPIKE_SCHEMA"
	// The child sinks: one that MERGEs in an open transaction of separate
	// requests, and one that sends the whole transaction as one request,
	// which waits before it records the offset.
	childOpenTransaction = "open-transaction"
	childOneRequest      = "one-request"
)

// runChild is the killed sink. It prints "ready" and its session at the
// moment to be killed, then waits.
func runChild() int {
	account, _, err := testAccountFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	account.Schema = os.Getenv(childSchemaEnv)
	db := Open(account)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	var session string
	if err == nil {
		err = conn.QueryRowContext(ctx, "SELECT CURRENT_SESSION()").Scan(&session)
	}
	batch := []change{{id: 1, name: "ada", amount: "99.00", offset: 10}}
	if err == nil {
		err = loadBatch(ctx, conn, batch)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch os.Getenv(childEnv) {
	case childOpenTransaction:
		for _, statement := range []string{"BEGIN", mergeBatch} {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		fmt.Println("ready", session)
	case childOneRequest:
		multi, err := sf.WithMultiStatement(ctx, 5)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("ready", session)
		// The request is on its way when the sink dies, waiting between the
		// MERGE and the offset.
		_, _ = conn.ExecContext(multi, "BEGIN; "+mergeBatch+"; SELECT SYSTEM$WAIT(15); "+mergeOffset+"; COMMIT;", topic, 11)
	}
	time.Sleep(time.Hour)
	return 0
}

// killChildWhenReady runs the child sink and SIGKILLs it once it is ready,
// after the extra wait, and returns its session. However the test ends, the
// child is killed and its session's transactions aborted, so that their
// locks do not outlast the test.
func killChildWhenReady(t *testing.T, db *sql.DB, kind, schema string, after time.Duration) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"="+kind, childSchemaEnv+"="+schema)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var session string
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if session == "" {
			return
		}
		ctx := context.WithoutCancel(t.Context())
		ids, err := transactionsOf(ctx, db, func(row map[string]string) bool { return row["session"] == session })
		for _, id := range ids {
			if err == nil {
				_, err = db.ExecContext(ctx, "SELECT SYSTEM$ABORT_TRANSACTION(?)", id)
			}
		}
		if err != nil {
			t.Errorf("abort the child sink's transactions: %v", err)
		}
	})
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			if session, ok := strings.CutPrefix(scanner.Text(), "ready "); ok {
				ready <- session
				return
			}
		}
		ready <- ""
	}()
	select {
	case session = <-ready:
		if session == "" {
			t.Fatal("the child sink failed before it was ready")
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("the child sink was not ready within 2 minutes")
	}
	time.Sleep(after)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return session
}

// transactionsOf returns the IDs of the test user's open transactions that
// match.
func transactionsOf(ctx context.Context, db *sql.DB, match func(row map[string]string) bool) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW TRANSACTIONS")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := map[string]string{}
		for i, column := range columns {
			row[strings.ToLower(column)] = values[i].String
		}
		if match(row) {
			ids = append(ids, row["id"])
		}
	}
	return ids, rows.Err()
}

// openTransactions returns the open transactions of the session. Other runs
// of the tests sign in as the same user, so their transactions are left
// alone; a sink, the only one with its user, aborts those of all its user's
// other sessions.
func openTransactions(t *testing.T, db *sql.DB, session string) []string {
	t.Helper()
	ids, err := transactionsOf(t.Context(), db, func(row map[string]string) bool { return row["session"] == session })
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// crashState returns the one row's amount and offset and the next offset,
// read in one statement, so that both come from the same moment.
func crashState(t *testing.T, db *sql.DB) string {
	t.Helper()
	var amount string
	var offset, next int64
	err := db.QueryRowContext(t.Context(), `SELECT T.AMOUNT, T._CHANGEPORT_OFFSET, O.NEXT_OFFSET
		FROM ORDERS T, _CHANGEPORT_OFFSETS O WHERE T.ID = 1 AND O.TOPIC = ?`, topic).Scan(&amount, &offset, &next)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("amount %s @%d, next offset %d", amount, offset, next)
}

func TestASinkKilledInAnOpenTransactionLeavesItHoldingLocks(t *testing.T) {
	db, schema := testAccount(t)
	setUpTables(t, db)
	applyBatch(t, db, []change{{id: 1, name: "ada", amount: "10.00", offset: 0}})

	dead := killChildWhenReady(t, db, childOpenTransaction, schema, 0)

	// The restarted sink's MERGE waits for the dead sink's lock, for as long
	// as the session's LOCK_TIMEOUT: 12 hours by default.
	ctx := t.Context()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "ALTER SESSION SET LOCK_TIMEOUT = 10"); err != nil {
		t.Fatal(err)
	}
	if err := loadBatch(ctx, conn, []change{{id: 1, name: "ada", amount: "12.00", offset: 1}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = commitBatch(ctx, conn, 2)
	t.Logf("the restarted sink's commit after %s: %v", time.Since(start).Round(time.Second), err)
	if err == nil {
		t.Fatal("the restarted sink committed while the dead sink's transaction was open; expected it to wait for the lock")
	}
	if !strings.Contains(err.Error(), "waiting for this lock is limited") {
		t.Fatalf("the restarted sink's commit failed, but not waiting for the lock: %v", err)
	}

	// It aborts the dead sink's transactions, which the same user may, and
	// goes on; the dead sink's MERGE never happened.
	ids := openTransactions(t, db, dead)
	if len(ids) == 0 {
		t.Fatal("the dead sink's session has no open transaction")
	}
	for _, id := range ids {
		var result string
		if err := db.QueryRowContext(ctx, "SELECT SYSTEM$ABORT_TRANSACTION(?)", id).Scan(&result); err != nil {
			t.Fatalf("abort transaction %s: %v", id, err)
		}
		t.Logf("abort transaction %s: %s", id, result)
	}
	if err := loadBatch(ctx, conn, []change{{id: 1, name: "ada", amount: "12.00", offset: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := commitBatch(ctx, conn, 2); err != nil {
		t.Fatalf("commit after aborting the dead sink's transaction: %v", err)
	}
	if got, want := rowsOf(t, db), "1 ada 12.00 @1 deleted=false"; got != want {
		t.Fatalf("rows %q, want %q", got, want)
	}
}

func TestASinkKilledDuringItsOneRequestTransactionLeavesItWhole(t *testing.T) {
	db, schema := testAccount(t)
	setUpTables(t, db)
	applyBatch(t, db, []change{{id: 1, name: "ada", amount: "10.00", offset: 0}})

	// Killed two seconds into a request that takes fifteen.
	dead := killChildWhenReady(t, db, childOneRequest, schema, 2*time.Second)

	// The server goes on with the request: after its wait, both the MERGE and
	// the offset are there, never one without the other. The design relies
	// on the server finishing it (docs/snowflake.md), so a request that
	// never commits fails too.
	before, after := "amount 10.00 @0, next offset 1", "amount 99.00 @10, next offset 11"
	deadline := time.Now().Add(2 * time.Minute)
	for {
		state := crashState(t, db)
		t.Logf("%s: %s", time.Now().Format(time.TimeOnly), state)
		switch {
		case state == after:
			if ids := openTransactions(t, db, dead); len(ids) > 0 {
				t.Fatalf("the dead sink's transaction committed, and left %d transactions open", len(ids))
			}
			return
		case state != before:
			t.Fatalf("%s: half a transaction", state)
		case time.Now().After(deadline):
			t.Fatal("the dead sink's transaction never committed")
		}
		time.Sleep(5 * time.Second)
	}
}
