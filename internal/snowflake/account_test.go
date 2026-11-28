package snowflake

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	sf "github.com/snowflakedb/gosnowflake"
)

// The test account's database, where each run makes a schema of its own
// (hack/snowflake-test-account.sql).
const testDatabase = "CHANGEPORT_TEST"

// abandonedAfter is how old a run's schema is when a later run drops it, as
// the run that made it crashed.
const abandonedAfter = time.Hour

func TestMain(m *testing.M) {
	_ = sf.GetLogger().SetLogLevel("error")
	// A test may run this binary again as a sink it kills (crash_test.go).
	if os.Getenv(childEnv) != "" {
		os.Exit(runChild())
	}
	os.Exit(m.Run())
}

// testAccountFromEnv is the Snowflake test account of the environment
// (README.md, Snowflake), if it has one.
func testAccountFromEnv() (Account, bool, error) {
	identifier, user, key := os.Getenv("SNOWFLAKE_ACCOUNT"), os.Getenv("SNOWFLAKE_TEST_USER"), os.Getenv("SNOWFLAKE_TEST_PRIVATE_KEY")
	if identifier == "" || user == "" || key == "" {
		return Account{}, false, nil
	}
	pemKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return Account{}, true, fmt.Errorf("SNOWFLAKE_TEST_PRIVATE_KEY is not base64: %w", err)
	}
	rsaKey, err := ParsePrivateKey(pemKey)
	if err != nil {
		return Account{}, true, err
	}
	return Account{Identifier: identifier, User: user, Key: rsaKey, Database: testDatabase}, true, nil
}

// testAccount opens the Snowflake test account and a schema of this run's
// own in its database, dropped when the test ends, after dropping those of
// runs that crashed. Without the account in the environment, it skips.
func testAccount(t *testing.T) (*sql.DB, string) {
	t.Helper()
	account, ok, err := testAccountFromEnv()
	if !ok {
		t.Skip("no Snowflake test account: set SNOWFLAKE_ACCOUNT, SNOWFLAKE_TEST_USER and SNOWFLAKE_TEST_PRIVATE_KEY (README.md, Snowflake)")
	}
	if err != nil {
		t.Fatal(err)
	}
	db := Open(account)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	// Another run's failure to clean up must not fail this one.
	if err := dropAbandoned(ctx, db); err != nil {
		t.Logf("drop the schemas of crashed runs: %v", err)
	}
	schema := runSchema()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+testDatabase+"."+schema); err != nil {
		t.Fatalf("create the run's schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+testDatabase+"."+schema); err != nil {
			t.Errorf("drop the run's schema: %v", err)
		}
	})
	account.Schema = schema
	inSchema := Open(account)
	t.Cleanup(func() { _ = inSchema.Close() })
	return inSchema, schema
}

// runSchema names a schema for one test: RUN_, the time in seconds and four
// random hex digits, so that runs on many machines never share one. It is
// upper case, as Snowflake keeps unquoted names.
func runSchema() string {
	random := make([]byte, 2)
	_, _ = rand.Read(random)
	return fmt.Sprintf("RUN_%s_%s", strings.ToUpper(strconv.FormatInt(time.Now().Unix(), 36)), strings.ToUpper(hex.EncodeToString(random)))
}

// dropAbandoned aborts the test user's transactions and drops the run
// schemas older than abandonedAfter, such as those of a run cancelled while
// its child sink's transaction was open. Runs on other machines may drop the
// same schema at once, so a schema already gone is fine.
func dropAbandoned(ctx context.Context, db *sql.DB) error {
	transactions, err := transactionsOf(ctx, db, func(row map[string]string) bool {
		started, err := time.Parse("2006-01-02T15:04:05.999-07:00", row["started_on"])
		return err == nil && time.Since(started) > abandonedAfter
	})
	if err != nil {
		return err
	}
	for _, id := range transactions {
		if _, err := db.ExecContext(ctx, "SELECT SYSTEM$ABORT_TRANSACTION(?)", id); err != nil {
			return fmt.Errorf("abort transaction %s: %w", id, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT SCHEMA_NAME FROM `+testDatabase+`.INFORMATION_SCHEMA.SCHEMATA
		WHERE SCHEMA_NAME LIKE 'RUN\\_%' AND CREATED < DATEADD(second, ?, CURRENT_TIMESTAMP())`, -int(abandonedAfter.Seconds()))
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+testDatabase+"."+name); err != nil && !strings.Contains(err.Error(), "does not exist") {
			return err
		}
	}
	return nil
}
