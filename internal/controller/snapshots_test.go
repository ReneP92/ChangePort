package controller

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hamba/avro/v2"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ReneP92/changeport/internal/provisioning"
)

// notificationSchema is Debezium 3.7's, as Apicurio registered it on kind.
const notificationSchema = `{"type":"record","name":"Notification","namespace":"io.debezium.connector.common","fields":[{"name":"id","type":"string"},{"name":"type","type":"string"},{"name":"aggregate_type","type":"string"},{"name":"additional_data","type":["null",{"type":"map","values":"string"}],"default":null},{"name":"timestamp","type":"long"}],"connect.version":1,"connect.name":"io.debezium.connector.common.Notification"}`

// wire encodes a notification as the Apicurio converter does: Confluent's
// wire format with schema ID 96.
func wire(t *testing.T, record notificationRecord) []byte {
	t.Helper()
	encoded, err := avro.Marshal(avro.MustParse(notificationSchema), record)
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[1:], 96)
	return append(header, encoded...)
}

func TestDecodeNotifications(t *testing.T) {
	requests := 0
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/apis/ccompat/v7/schemas/ids/96" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"schema": notificationSchema})
	}))
	defer registry.Close()
	snapshots := &KafkaSnapshots{RegistryURL: func(provisioning.Spec) string { return registry.URL }}
	source := newSource()

	data := map[string]string{"scanned_collection": "public.orders", "total_rows_scanned": "1000", "status": "SUCCEEDED"}
	got, err := snapshots.decode(t.Context(), source, wire(t, notificationRecord{
		ID: "1", Type: "TABLE_SCAN_COMPLETED", AggregateType: incrementalSnapshot, AdditionalData: &data, Timestamp: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "TABLE_SCAN_COMPLETED" || got.AggregateType != incrementalSnapshot || got.Data["total_rows_scanned"] != "1000" {
		t.Errorf("decode() = %+v", got)
	}

	// Without additional data, from the cached schema.
	got, err = snapshots.decode(t.Context(), source, wire(t, notificationRecord{ID: "2", Type: "COMPLETED", AggregateType: incrementalSnapshot}))
	if err != nil || got.Type != "COMPLETED" || got.Data != nil || requests != 1 {
		t.Errorf("decode() = %+v, %v after %d schema requests", got, err, requests)
	}

	// A tombstone, or a value in another format, is no notification.
	for _, value := range [][]byte{nil, []byte("{}")} {
		if got, err := snapshots.decode(t.Context(), source, value); got != nil || err != nil {
			t.Errorf("decode(%q) = %+v, %v", value, got, err)
		}
	}
}

// The connector writes its notifications in transactions: the reader skips
// aborted ones, steps over the markers that end each transaction, and stops
// at the last stable offset, before a transaction still open.
func TestReadNotificationsInTransactions(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "notifications"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.TransactionalID("connector"), kgo.DefaultProduceTopic("notifications"))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	// write begins a transaction with value; end ends it.
	write := func(value string) {
		t.Helper()
		if err := producer.BeginTransaction(); err != nil {
			t.Fatal(err)
		}
		if err := producer.ProduceSync(t.Context(), kgo.StringRecord(value)).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
	end := func(commit kgo.TransactionEndTry) {
		t.Helper()
		if err := producer.EndTransaction(t.Context(), commit); err != nil {
			t.Fatal(err)
		}
	}
	write("first")
	end(kgo.TryCommit)
	write("aborted")
	end(kgo.TryAbort)
	write("second")
	end(kgo.TryCommit)
	write("open")

	read := func(from int64) ([]string, int64) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		consumer, err := kgo.NewClient(append(committedOnly(), kgo.SeedBrokers(cluster.ListenAddrs()...),
			kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{"notifications": {0: kgo.NewOffset().At(from)}}))...)
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		notifications, next, err := readNotifications(ctx, consumer, "notifications", from, func(value []byte) (*Notification, error) {
			return &Notification{Type: string(value)}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var types []string
		for _, notification := range notifications {
			types = append(types, notification.Type)
		}
		return types, next
	}
	// first 0, its marker 1, aborted 2, its marker 3, second 4, its marker
	// 5, and open 6, past the last stable offset.
	types, next := read(0)
	if !slices.Equal(types, []string{"first", "second"}) || next != 6 {
		t.Errorf("read %v up to %d, want first and second up to 6", types, next)
	}
	if types, again := read(next); len(types) != 0 || again != next {
		t.Errorf("read again %v up to %d, want nothing up to %d", types, again, next)
	}
}
