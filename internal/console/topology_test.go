package console

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ReneP92/changeport/internal/metrics"
)

func series(value float64, labels ...string) metrics.Series {
	s := metrics.Series{Labels: map[string]string{}, Points: []metrics.Point{{Time: time.Now(), Value: value}}}
	for i := 0; i+1 < len(labels); i += 2 {
		s.Labels[labels[i]] = labels[i+1]
	}
	return s
}

func TestTopologyShowsHowDataFlows(t *testing.T) {
	settings := []TopicSettings{{Name: "orders"}, {Name: "payments"}, {Name: "idle"}, {Name: "shop.public.orders"}, {Name: "__consumer_offsets"}}
	members := map[string][]string{
		"billing":               {"billing-1", "billing-2"},
		"fresh":                 {"fresh-1"},
		"empty":                 {},
		"changeport-registry-x": {"apicurio"},
	}
	values := map[metrics.Query][]metrics.Series{
		metrics.BytesIn: {
			series(1000, "topic", "orders"),
			// Most of payments' traffic comes from a client without telemetry.
			series(5000, "topic", "payments"),
			series(80, "topic", "shop.public.orders"),
		},
		metrics.BytesOut: {series(900, "topic", "orders")},
		metrics.ProducerBytes: {
			series(950, "topic", "orders", "principal", "User:admin", "client_id", "checkout"),
			series(300, "topic", "payments", "principal", "User:admin", "client_id", "checkout"),
			// A topic that is gone since.
			series(10, "topic", "deleted", "principal", "User:admin", "client_id", "checkout"),
		},
		metrics.ConsumerBytes: {
			series(400, "topic", "orders", "principal", "User:admin", "client_id", "billing-1"),
			series(500, "topic", "orders", "principal", "User:admin", "client_id", "billing-2"),
			// Java consumers report a topic twice, once with dots replaced.
			series(500, "topic", "shop_public_orders", "principal", "User:admin", "client_id", "billing-2"),
		},
		metrics.LagSeconds: {
			series(5, "consumer_group", "billing", "topic", "orders"),
			series(240, "consumer_group", "billing", "topic", "payments"),
			series(1, "consumer_group", "changeport-registry-x", "topic", "orders"),
		},
		metrics.LagMessages: {
			series(10, "consumer_group", "billing", "topic", "orders"),
			series(300, "consumer_group", "billing", "topic", "payments"),
		},
		metrics.LagCapped: {series(1, "consumer_group", "billing", "topic", "payments")},
	}
	got := buildTopology(settings, members, map[string]string{"shop.public.orders": "shop"}, values)
	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "groups": [
    {
      "lagMessages": 310,
      "lagSeconds": 240,
      "members": 2,
      "name": "billing"
    },
    {
      "members": 1,
      "name": "fresh"
    }
  ],
  "producers": [
    {
      "bytesPerSecond": 1250,
      "clientId": "checkout",
      "id": "User:admin/checkout",
      "principal": "User:admin"
    }
  ],
  "reads": [
    {
      "bytesPerSecond": 900,
      "capped": false,
      "group": "billing",
      "lagMessages": 10,
      "lagSeconds": 5,
      "topic": "orders"
    },
    {
      "capped": true,
      "group": "billing",
      "lagMessages": 300,
      "lagSeconds": 240,
      "topic": "payments"
    }
  ],
  "topics": [
    {
      "bytesInPerSecond": 0,
      "bytesOutPerSecond": 0,
      "name": "idle",
      "unreportedBytesInPerSecond": 0
    },
    {
      "bytesInPerSecond": 1000,
      "bytesOutPerSecond": 900,
      "name": "orders",
      "unreportedBytesInPerSecond": 0
    },
    {
      "bytesInPerSecond": 5000,
      "bytesOutPerSecond": 0,
      "name": "payments",
      "unreportedBytesInPerSecond": 4700
    },
    {
      "bytesInPerSecond": 80,
      "bytesOutPerSecond": 0,
      "name": "shop.public.orders",
      "source": "shop",
      "unreportedBytesInPerSecond": 0
    }
  ],
  "writes": [
    {
      "bytesPerSecond": 950,
      "producer": "User:admin/checkout",
      "topic": "orders"
    },
    {
      "bytesPerSecond": 300,
      "producer": "User:admin/checkout",
      "topic": "payments"
    }
  ]
}`
	if string(encoded) != want {
		t.Errorf("buildTopology() =\n%s\nwant\n%s", encoded, want)
	}
}
