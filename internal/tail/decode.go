// Package tail decodes the messages the console shows admins of a topic
// (docs/console.md): Avro in Confluent's wire format with the schemas of the
// customer's registry, JSON, text, or else binary, shown as hex. Each part
// is cut at a limit, so that one large message cannot flood the browser.
// Nothing is kept: messages pass through on their way to the browser.
package tail

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hamba/avro/v2"
)

// Format is how a part of a message is shown.
type Format string

// The formats.
const (
	JSON   Format = "json"
	Text   Format = "text"
	Binary Format = "binary"
	// Avro is Avro in Confluent's wire format, decoded to JSON with the
	// registry's schema.
	Avro Format = "avro"
)

// Payload is a key, value or header value as shown: its text, for binary
// the hex of its bytes, cut at the limit.
type Payload struct {
	Format    Format
	Text      string
	Size      int
	Truncated bool
	// SchemaID is the registry's ID of an Avro payload's schema.
	SchemaID uint32
}

// Schemas looks Avro schemas up by their registry ID.
type Schemas interface {
	Schema(ctx context.Context, id uint32) (avro.Schema, error)
}

// Decoder decodes the parts of messages of one customer's cluster.
type Decoder struct {
	// Schemas is the customer's registry, if it has one.
	Schemas Schemas
	// Limit is how many bytes of text, or of binary, a part shows at most.
	Limit int
}

// Decode shows data, or nothing for a missing part.
func (d Decoder) Decode(ctx context.Context, data []byte) *Payload {
	if data == nil {
		return nil
	}
	if p, ok := d.avro(ctx, data); ok {
		return p
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		return d.text(JSON, data)
	}
	if printable(data) {
		return d.text(Text, data)
	}
	shown := data[:min(len(data), d.Limit/2)]
	return &Payload{Format: Binary, Text: hex.EncodeToString(shown), Size: len(data), Truncated: len(shown) < len(data)}
}

// avro decodes Confluent's wire format: a zero byte, the schema's ID in
// four bytes, then Avro.
func (d Decoder) avro(ctx context.Context, data []byte) (*Payload, bool) {
	if d.Schemas == nil || len(data) < 5 || data[0] != 0 {
		return nil, false
	}
	id := binary.BigEndian.Uint32(data[1:5])
	schema, err := d.Schemas.Schema(ctx, id)
	if err != nil {
		return nil, false
	}
	var decoded any
	if err := avro.Unmarshal(schema, data[5:], &decoded); err != nil {
		return nil, false
	}
	text, err := json.Marshal(plain(schema, decoded))
	if err != nil {
		return nil, false
	}
	p := d.text(Avro, text)
	p.Size, p.SchemaID = len(data), id
	return p, true
}

// text cuts data at the limit, at a character's boundary.
func (d Decoder) text(format Format, data []byte) *Payload {
	shown := data
	if len(shown) > d.Limit {
		shown = shown[:d.Limit]
		for len(shown) > 0 && !utf8.Valid(shown) {
			shown = shown[:len(shown)-1]
		}
	}
	return &Payload{Format: format, Text: string(shown), Size: len(data), Truncated: len(shown) < len(data)}
}

// printable reports whether data is UTF-8 text without control characters
// other than whitespace.
func printable(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

// Registry looks schemas up in a customer's schema registry through its
// Confluent-compatible API, and keeps them: a schema's ID never changes
// what it names. A registry that does not answer, such as for a customer
// without Postgres sources, is not asked again for a minute.
type Registry struct {
	// URL is the registry's, such as
	// http://changeport-registry.customer-acme.svc:8080.
	URL  string
	HTTP *http.Client

	mu      sync.Mutex
	schemas map[uint32]avro.Schema
	down    time.Time
}

// Schema implements Schemas.
func (r *Registry) Schema(ctx context.Context, id uint32) (avro.Schema, error) {
	r.mu.Lock()
	if schema, ok := r.schemas[id]; ok {
		r.mu.Unlock()
		return schema, nil
	}
	if time.Since(r.down) < time.Minute {
		r.mu.Unlock()
		return nil, fmt.Errorf("the registry at %s did not answer lately", r.URL)
	}
	r.mu.Unlock()
	schema, err := r.fetch(ctx, id)
	r.mu.Lock()
	defer r.mu.Unlock()
	if errors.Is(err, errUnreachable) {
		r.down = time.Now()
	}
	if err != nil {
		return nil, err
	}
	if r.schemas == nil {
		r.schemas = map[uint32]avro.Schema{}
	}
	r.schemas[id] = schema
	return schema, nil
}

// errUnreachable means the registry did not answer at all.
var errUnreachable = errors.New("the registry did not answer")

// registered is a schema as the registry answers for it: its text, and the
// schemas it refers to by name, which Apicurio registers apart, such as the
// row's record and Debezium's source of a change event.
type registered struct {
	Schema     string `json:"schema"`
	References []struct {
		Name    string `json:"name"`
		Subject string `json:"subject"`
		Version int    `json:"version"`
	} `json:"references"`
}

// maxReferences caps how many schemas one schema may refer to, through
// each other, so that a cycle cannot loop.
const maxReferences = 50

func (r *Registry) fetch(ctx context.Context, id uint32) (avro.Schema, error) {
	var top registered
	if err := r.get(ctx, fmt.Sprintf("/schemas/ids/%d", id), &top); err != nil {
		return nil, fmt.Errorf("fetch schema %d: %w", id, err)
	}
	// The referred schemas go into the cache first, so that the schema's
	// names resolve to them.
	cache := &avro.SchemaCache{}
	seen := map[string]bool{}
	if err := r.parseReferences(ctx, top, cache, seen); err != nil {
		return nil, fmt.Errorf("schema %d: %w", id, err)
	}
	return avro.ParseWithCache(top.Schema, "", cache)
}

func (r *Registry) parseReferences(ctx context.Context, schema registered, cache *avro.SchemaCache, seen map[string]bool) error {
	for _, ref := range schema.References {
		key := fmt.Sprintf("%s/%d", ref.Subject, ref.Version)
		if seen[key] {
			continue
		}
		if seen[key] = true; len(seen) > maxReferences {
			return fmt.Errorf("it refers to more than %d schemas", maxReferences)
		}
		var referred registered
		if err := r.get(ctx, fmt.Sprintf("/subjects/%s/versions/%d", url.PathEscape(ref.Subject), ref.Version), &referred); err != nil {
			return fmt.Errorf("fetch its reference %s: %w", ref.Name, err)
		}
		if err := r.parseReferences(ctx, referred, cache, seen); err != nil {
			return err
		}
		if _, err := avro.ParseWithCache(referred.Schema, "", cache); err != nil {
			return fmt.Errorf("parse its reference %s: %w", ref.Name, err)
		}
	}
	return nil
}

// get reads one of the registry's answers into out.
func (r *Registry) get(ctx context.Context, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL+"/apis/ccompat/v7"+path, http.NoBody)
	if err != nil {
		return err
	}
	httpClient := r.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", path, response.Status)
	}
	return json.Unmarshal(body, out)
}

// plain turns a value decoded with schema into plain JSON: the decoder
// keeps a union's record or other named branch under the branch's name,
// such as {"shop.public.orders.Value": {...}}, which JSON readers do not
// expect.
//
// It also writes decimals with their scale, such as "96.90", where the
// decoder gives fractions, such as 969/10.
func plain(schema avro.Schema, v any) any {
	switch s := schema.(type) {
	case *avro.RefSchema:
		return plain(s.Schema(), v)
	case *avro.PrimitiveSchema:
		return decimal(s.Logical(), v)
	case *avro.FixedSchema:
		return decimal(s.Logical(), v)
	case *avro.RecordSchema:
		fields, ok := v.(map[string]any)
		if !ok {
			return v
		}
		for _, f := range s.Fields() {
			if value, ok := fields[f.Name()]; ok {
				fields[f.Name()] = plain(f.Type(), value)
			}
		}
		return fields
	case *avro.ArraySchema:
		items, ok := v.([]any)
		if !ok {
			return v
		}
		for i := range items {
			items[i] = plain(s.Items(), items[i])
		}
		return items
	case *avro.MapSchema:
		values, ok := v.(map[string]any)
		if !ok {
			return v
		}
		for key := range values {
			values[key] = plain(s.Values(), values[key])
		}
		return values
	case *avro.UnionSchema:
		if v == nil {
			return nil
		}
		if wrapped, ok := v.(map[string]any); ok && len(wrapped) == 1 {
			for _, branch := range s.Types() {
				if inner, ok := wrapped[branchName(branch)]; ok {
					return plain(branch, inner)
				}
			}
		}
		// Not wrapped: a branch the decoder resolved, such as a string; one
		// other than null is the one.
		var others []avro.Schema
		for _, branch := range s.Types() {
			if branch.Type() != avro.Null {
				others = append(others, branch)
			}
		}
		if len(others) == 1 {
			return plain(others[0], v)
		}
	}
	return v
}

// branchName is the name the decoder keeps a union's branch under: a named
// type's full name, else its type.
func branchName(schema avro.Schema) string {
	if ref, ok := schema.(*avro.RefSchema); ok {
		schema = ref.Schema()
	}
	if named, ok := schema.(avro.NamedSchema); ok {
		return named.FullName()
	}
	return string(schema.Type())
}

// decimal writes a decimal at its scale.
func decimal(logical avro.LogicalSchema, v any) any {
	d, ok := logical.(*avro.DecimalLogicalSchema)
	if !ok {
		return v
	}
	if r, ok := v.(*big.Rat); ok {
		return r.FloatString(d.Scale())
	}
	return v
}
