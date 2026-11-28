package snowflake

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	sf "github.com/snowflakedb/gosnowflake"
)

// The spike's measurements of loading a batch (#219, docs/snowflake.md): bound
// arrays into a temporary table against a CSV file PUT on a temporary stage
// and COPYed into it, then the MERGE of each batch, at three sizes. They use
// credits and measure rather than check, so they run only with
// SNOWFLAKE_SPIKE=1.

// The measured table is about as wide as a typical application table: a
// key, a name, an amount, a timestamp, a flag and a 200-character note.
const (
	createLoadTarget = `CREATE TABLE IF NOT EXISTS LOAD_TARGET (
		ID NUMBER(38,0) NOT NULL PRIMARY KEY, NAME TEXT, AMOUNT NUMBER(18,2), CREATED TIMESTAMP_NTZ,
		ACTIVE BOOLEAN, NOTES TEXT, _CHANGEPORT_OFFSET NUMBER(38,0) NOT NULL)`
	createLoadBatch = `CREATE OR REPLACE TEMPORARY TABLE LOAD_BATCH (
		ID NUMBER(38,0), NAME TEXT, AMOUNT NUMBER(18,2), CREATED TIMESTAMP_NTZ,
		ACTIVE BOOLEAN, NOTES TEXT, _CHANGEPORT_OFFSET NUMBER(38,0))`
	mergeLoadBatch = `MERGE INTO LOAD_TARGET T USING (
			SELECT * FROM LOAD_BATCH QUALIFY ROW_NUMBER() OVER (PARTITION BY ID ORDER BY _CHANGEPORT_OFFSET DESC) = 1
		) S ON T.ID = S.ID
		WHEN MATCHED AND S._CHANGEPORT_OFFSET > T._CHANGEPORT_OFFSET THEN UPDATE SET
			NAME = S.NAME, AMOUNT = S.AMOUNT, CREATED = S.CREATED, ACTIVE = S.ACTIVE, NOTES = S.NOTES,
			_CHANGEPORT_OFFSET = S._CHANGEPORT_OFFSET
		WHEN NOT MATCHED THEN INSERT VALUES (S.ID, S.NAME, S.AMOUNT, S.CREATED, S.ACTIVE, S.NOTES, S._CHANGEPORT_OFFSET)`
)

type loadColumns struct {
	ids, offsets   []int64
	names, amounts []string
	notes          []string
	created        []time.Time
	active         []bool
}

// rowsFor makes n rows with IDs from 1, at offsets from first.
func rowsFor(n int, first int64) loadColumns {
	c := loadColumns{
		ids: make([]int64, n), offsets: make([]int64, n), names: make([]string, n), amounts: make([]string, n),
		notes: make([]string, n), created: make([]time.Time, n), active: make([]bool, n),
	}
	note := strings.Repeat("a note about this order, ", 8)[:200]
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		c.ids[i] = int64(i + 1)
		c.offsets[i] = first + int64(i)
		c.names[i] = "customer " + strconv.Itoa(i)
		c.amounts[i] = strconv.Itoa(i%10000) + "." + strconv.Itoa(10+i%90)
		c.notes[i] = note
		c.created[i] = start.Add(time.Duration(i) * time.Second)
		c.active[i] = i%2 == 0
	}
	return c
}

func loadWithArrays(ctx context.Context, conn *sql.Conn, c loadColumns) error {
	_, err := conn.ExecContext(ctx, "INSERT INTO LOAD_BATCH VALUES (?, ?, ?, ?, ?, ?, ?)",
		sf.Array(&c.ids), sf.Array(&c.names), sf.Array(&c.amounts), sf.Array(&c.created, sf.TimestampNTZType),
		sf.Array(&c.active), sf.Array(&c.notes), sf.Array(&c.offsets))
	return err
}

func loadWithCopy(ctx context.Context, conn *sql.Conn, c loadColumns) error {
	var file bytes.Buffer
	w := csv.NewWriter(&file)
	for i := range c.ids {
		if err := w.Write([]string{
			strconv.FormatInt(c.ids[i], 10), c.names[i], c.amounts[i], c.created[i].Format("2006-01-02 15:04:05"),
			strconv.FormatBool(c.active[i]), c.notes[i], strconv.FormatInt(c.offsets[i], 10),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	if _, err := conn.ExecContext(ctx, "CREATE TEMPORARY STAGE IF NOT EXISTS LOAD_STAGE"); err != nil {
		return err
	}
	put := sf.WithFileStream(ctx, &file)
	if _, err := conn.ExecContext(put, "PUT 'file:///batch.csv' @LOAD_STAGE AUTO_COMPRESS = TRUE OVERWRITE = TRUE"); err != nil {
		return fmt.Errorf("put: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COPY INTO LOAD_BATCH FROM @LOAD_STAGE/batch.csv.gz
		FILE_FORMAT = (TYPE = CSV FIELD_OPTIONALLY_ENCLOSED_BY = '"') PURGE = TRUE`)
	return err
}

func timed(t *testing.T, what string, f func() error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := f(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	return time.Since(start)
}

func TestMeasureLoadingAndMerging(t *testing.T) {
	if os.Getenv("SNOWFLAKE_SPIKE") == "" {
		t.Skip("measures with credits; set SNOWFLAKE_SPIKE=1")
	}
	db, _ := testAccount(t)
	ctx := t.Context()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, createLoadTarget); err != nil {
		t.Fatal(err)
	}
	// The warehouse resumes before anything is timed.
	timed(t, "resume", func() error {
		_, err := conn.ExecContext(ctx, "SELECT COUNT(*) FROM LOAD_TARGET")
		return err
	})

	var report []string
	var offset int64
	for _, n := range []int{1_000, 10_000, 100_000} {
		c := rowsFor(n, offset)
		offset += int64(n)
		arrays := timed(t, "arrays", func() error {
			if _, err := conn.ExecContext(ctx, createLoadBatch); err != nil {
				return err
			}
			return loadWithArrays(ctx, conn, c)
		})
		copied := timed(t, "copy", func() error {
			if _, err := conn.ExecContext(ctx, createLoadBatch); err != nil {
				return err
			}
			return loadWithCopy(ctx, conn, c)
		})
		var count int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM LOAD_BATCH").Scan(&count); err != nil || count != n {
			t.Fatalf("the batch holds %d rows (%v), want %d", count, err, n)
		}
		// The rows are new the first time, and updates of every row the
		// second, with later offsets.
		inserts := timed(t, "merge inserts", func() error {
			_, err := conn.ExecContext(ctx, "TRUNCATE TABLE LOAD_TARGET")
			if err == nil {
				_, err = conn.ExecContext(ctx, mergeLoadBatch)
			}
			return err
		})
		updates := timed(t, "merge updates", func() error {
			_, err := conn.ExecContext(ctx, "UPDATE LOAD_BATCH SET _CHANGEPORT_OFFSET = _CHANGEPORT_OFFSET + 1000000, AMOUNT = AMOUNT + 1")
			if err == nil {
				_, err = conn.ExecContext(ctx, mergeLoadBatch)
			}
			return err
		})
		report = append(report, fmt.Sprintf("%7d rows: arrays %6s  put+copy %6s  merge inserts %6s  merge updates %6s",
			n, arrays.Round(10*time.Millisecond), copied.Round(10*time.Millisecond),
			inserts.Round(10*time.Millisecond), updates.Round(10*time.Millisecond)))
	}
	t.Log("\n" + strings.Join(report, "\n"))
}

// TestMeasureMergingIntoALargeTable measures the MERGE of a batch into a
// table of ten million rows: Snowflake has no indexes, so a MERGE reads the
// micro-partitions that may hold the batch's keys, and keys spread over the
// whole table cost more than recent ones, as an application's updates
// mostly are.
func TestMeasureMergingIntoALargeTable(t *testing.T) {
	if os.Getenv("SNOWFLAKE_SPIKE") == "" {
		t.Skip("measures with credits; set SNOWFLAKE_SPIKE=1")
	}
	db, _ := testAccount(t)
	ctx := t.Context()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	const size = 10_000_000
	created := timed(t, "create", func() error {
		_, err := conn.ExecContext(ctx, `CREATE TABLE LOAD_TARGET AS SELECT
			SEQ8() + 1 AS ID, 'customer ' || SEQ8() AS NAME, UNIFORM(0, 1000000, RANDOM()) / 100 :: NUMBER(18,2) AS AMOUNT,
			DATEADD(second, SEQ8(), '2020-01-01') :: TIMESTAMP_NTZ AS CREATED, SEQ8() % 2 = 0 AS ACTIVE,
			RANDSTR(200, RANDOM()) AS NOTES, SEQ8() AS _CHANGEPORT_OFFSET
			FROM TABLE(GENERATOR(ROWCOUNT => `+strconv.Itoa(size)+`))`)
		return err
	})
	t.Logf("created %d rows in %s", size, created.Round(100*time.Millisecond))

	var report []string
	for _, c := range []struct{ what, keys string }{
		{"keys over the whole table", fmt.Sprintf("UNIFORM(1, %d, RANDOM())", size)},
		{"the newest 1%'s keys", fmt.Sprintf("UNIFORM(%d, %d, RANDOM())", size-size/100, size)},
		{"new keys", fmt.Sprintf("%d + SEQ8()", size+1)},
	} {
		for _, n := range []int{1_000, 10_000, 100_000} {
			if _, err := conn.ExecContext(ctx, createLoadBatch); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO LOAD_BATCH SELECT `+c.keys+`, 'changed', 1.00,
				CURRENT_TIMESTAMP() :: TIMESTAMP_NTZ, TRUE, RANDSTR(200, RANDOM()), 100000000 + SEQ8()
				FROM TABLE(GENERATOR(ROWCOUNT => `+strconv.Itoa(n)+`))`); err != nil {
				t.Fatal(err)
			}
			merged := timed(t, "merge", func() error {
				_, err := conn.ExecContext(ctx, mergeLoadBatch)
				return err
			})
			report = append(report, fmt.Sprintf("%-26s %7d changes: merge %6s", c.what, n, merged.Round(10*time.Millisecond)))
		}
	}
	t.Log("\n" + strings.Join(report, "\n"))
}
