package tail

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hamba/avro/v2"
)

const orderSchema = `{"type":"record","name":"Value","fields":[{"name":"id","type":"int"},{"name":"note","type":["null","string"],"default":null}]}`

// wire encodes value in Confluent's wire format with schema ID id.
func wire(t *testing.T, id uint32, value any) []byte {
	t.Helper()
	data, err := avro.Marshal(avro.MustParse(orderSchema), value)
	if err != nil {
		t.Fatal(err)
	}
	prefix := []byte{0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(prefix[1:], id)
	return append(prefix, data...)
}

func TestDecodeShowsEachFormat(t *testing.T) {
	var asked atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/apis/ccompat/v7/schemas/ids/7" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"schema":` + jsonString(orderSchema) + `}`))
	}))
	defer registry.Close()
	d := Decoder{Schemas: &Registry{URL: registry.URL}, Limit: 32}

	for _, c := range []struct {
		name string
		data []byte
		want Payload
		none bool
	}{
		{name: "nothing", data: nil, none: true},
		{name: "JSON", data: []byte(`{"id": 1}`), want: Payload{Format: JSON, Text: `{"id": 1}`, Size: 9}},
		{name: "a JSON number is text", data: []byte(`42`), want: Payload{Format: Text, Text: "42", Size: 2}},
		{name: "text", data: []byte("hello\tworld\n"), want: Payload{Format: Text, Text: "hello\tworld\n", Size: 12}},
		{name: "long text, cut at a character", data: []byte(strings.Repeat("é", 20)), want: Payload{Format: Text, Text: strings.Repeat("é", 16), Size: 40, Truncated: true}},
		{name: "binary", data: []byte{0xff, 0x00, 0x10}, want: Payload{Format: Binary, Text: "ff0010", Size: 3}},
		{name: "long binary", data: bytes.Repeat([]byte{0xff}, 20), want: Payload{Format: Binary, Text: strings.Repeat("ff", 16), Size: 20, Truncated: true}},
		{
			name: "Avro", data: wire(t, 7, map[string]any{"id": 5, "note": map[string]any{"string": "x"}}),
			want: Payload{Format: Avro, Text: `{"id":5,"note":"x"}`, Size: 9, SchemaID: 7},
		},
		// A schema the registry does not know: the bytes as they are.
		{name: "Avro without its schema", data: wire(t, 9, map[string]any{"id": 1}), want: Payload{Format: Binary, Text: "00000000090200", Size: 7}},
	} {
		got := d.Decode(t.Context(), c.data)
		if c.none {
			if got != nil {
				t.Errorf("%s: Decode() = %+v, want nothing", c.name, got)
			}
			continue
		}
		if got == nil || *got != c.want {
			t.Errorf("%s: Decode() = %+v, want %+v", c.name, got, c.want)
		}
	}
	// Each schema is fetched once.
	d.Decode(t.Context(), wire(t, 7, map[string]any{"id": 6}))
	if asked.Load() != 2 {
		t.Errorf("asked the registry %d times, want once per schema", asked.Load())
	}
}

func TestAnUnreachableRegistryIsNotAskedAgainAtOnce(t *testing.T) {
	registry := httptest.NewServer(http.NotFoundHandler())
	url := registry.URL
	registry.Close()
	r := &Registry{URL: url}
	d := Decoder{Schemas: r, Limit: 64}
	for range 2 {
		if got := d.Decode(t.Context(), wire(t, 7, map[string]any{"id": 1})); got.Format != Binary {
			t.Errorf("Decode() without a registry = %+v, want the bytes", got)
		}
	}
	if r.down.IsZero() {
		t.Error("the registry was not marked down")
	}
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Apicurio registers the records a change event refers to apart, and the
// event's schema refers to them by name, as Debezium's envelope does.
func TestDecodeResolvesTheSchemasASchemaRefersTo(t *testing.T) {
	const value = `{"type":"record","name":"Value","namespace":"shop.public.orders","fields":[{"name":"id","type":"int"}]}`
	const source = `{"type":"record","name":"Source","namespace":"io.debezium","fields":[{"name":"table","type":"string"}]}`
	const envelope = `{"type":"record","name":"Envelope","namespace":"shop.public.orders","fields":[` +
		`{"name":"before","type":["null","Value"],"default":null},{"name":"after","type":["null","Value"],"default":null},` +
		`{"name":"source","type":"io.debezium.Source"},{"name":"op","type":"string"}]}`
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/ccompat/v7/schemas/ids/12":
			_, _ = w.Write([]byte(`{"schema":` + jsonString(envelope) + `,"references":[` +
				`{"name":"shop.public.orders.Value","subject":"shop.public.orders.Value","version":1},` +
				`{"name":"io.debezium.Source","subject":"io.debezium.Source","version":2}]}`))
		case "/apis/ccompat/v7/subjects/shop.public.orders.Value/versions/1":
			_, _ = w.Write([]byte(`{"schema":` + jsonString(value) + `}`))
		case "/apis/ccompat/v7/subjects/io.debezium.Source/versions/2":
			_, _ = w.Write([]byte(`{"schema":` + jsonString(source) + `}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer registry.Close()
	cache := &avro.SchemaCache{}
	for _, s := range []string{value, source} {
		if _, err := avro.ParseWithCache(s, "", cache); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := avro.ParseWithCache(envelope, "", cache)
	if err != nil {
		t.Fatal(err)
	}
	data, err := avro.Marshal(schema, map[string]any{
		"before": nil, "after": map[string]any{"shop.public.orders.Value": map[string]any{"id": 3}},
		"source": map[string]any{"table": "orders"}, "op": "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := Decoder{Schemas: &Registry{URL: registry.URL}, Limit: 1024}.Decode(t.Context(), append([]byte{0, 0, 0, 0, 12}, data...))
	if got.Format != Avro || got.Text != `{"after":{"id":3},"before":null,"op":"c","source":{"table":"orders"}}` {
		t.Errorf("Decode() = %+v, want the event as JSON", got)
	}
}

func TestDecimalsShowAtTheirScale(t *testing.T) {
	const schema = `{"type":"record","name":"Row","fields":[{"name":"amount","type":["null",{"type":"bytes","logicalType":"decimal","precision":10,"scale":2}]}]}`
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema":` + jsonString(schema) + `}`))
	}))
	defer registry.Close()
	data, err := avro.Marshal(avro.MustParse(schema), map[string]any{"amount": map[string]any{"bytes.decimal": big.NewRat(969, 10)}})
	if err != nil {
		t.Fatal(err)
	}
	got := Decoder{Schemas: &Registry{URL: registry.URL}, Limit: 1024}.Decode(t.Context(), append([]byte{0, 0, 0, 0, 1}, data...))
	if got.Text != `{"amount":"96.90"}` {
		t.Errorf("Decode() = %+v, want the amount at its scale", got)
	}
}
