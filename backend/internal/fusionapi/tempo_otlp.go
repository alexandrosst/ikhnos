package fusionapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---- OTLP/JSON, as Tempo returns it ----

type otlpValue struct {
	String *string                       `json:"stringValue"`
	Int    *json.Number                  `json:"intValue"`
	Double json.RawMessage               `json:"doubleValue"` // a number, or the string "NaN", "Infinity" or "-Infinity"
	Bool   *bool                         `json:"boolValue"`
	Bytes  *string                       `json:"bytesValue"`
	Array  *struct{ Values []otlpValue } `json:"arrayValue"`
	KV     *struct{ Values []otlpKV }    `json:"kvlistValue"`
}

type otlpKV struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

func (v otlpValue) any() any {
	switch {
	case v.String != nil:
		return *v.String
	case v.Int != nil:
		if n, err := v.Int.Int64(); err == nil {
			return n
		}
		return v.Int.String()
	case len(v.Double) > 0 && string(v.Double) != "null":
		var f float64
		if json.Unmarshal(v.Double, &f) == nil {
			return f
		}
		var s string // a non-finite value cannot be a JSON number, and must not be one in the answer either
		if json.Unmarshal(v.Double, &s) == nil {
			return s
		}
		return string(v.Double)
	case v.Bool != nil:
		return *v.Bool
	case v.Bytes != nil:
		return *v.Bytes
	case v.Array != nil:
		out := make([]any, 0, len(v.Array.Values))
		for _, e := range v.Array.Values {
			out = append(out, e.any())
		}
		return out
	case v.KV != nil:
		return kvMap(v.KV.Values)
	}
	return nil
}

func kvMap(kvs []otlpKV) map[string]any {
	if len(kvs) == 0 {
		return nil
	}
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.any()
	}
	return m
}

type otlpSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Name         string          `json:"name"`
	Kind         json.RawMessage `json:"kind"`
	Start        json.Number     `json:"startTimeUnixNano"`
	End          json.Number     `json:"endTimeUnixNano"`
	Attributes   []otlpKV        `json:"attributes"`
	Status       struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"status"`
	Events []struct {
		Time       json.Number `json:"timeUnixNano"`
		Name       string      `json:"name"`
		Attributes []otlpKV    `json:"attributes"`
	} `json:"events"`
}

type otlpResourceSpans struct {
	Resource struct {
		Attributes []otlpKV `json:"attributes"`
	} `json:"resource"`
	ScopeSpans []struct {
		Spans []otlpSpan `json:"spans"`
	} `json:"scopeSpans"`
	// Older Tempo and the OTLP draft spelled scopeSpans this way.
	LibrarySpans []struct {
		Spans []otlpSpan `json:"spans"`
	} `json:"instrumentationLibrarySpans"`
}

// traceBody holds the shapes Tempo has answered a trace read with: v2 wraps it in "trace", v1 uses "batches".
type traceBody struct {
	Trace *struct {
		ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
	} `json:"trace"`
	Batches       []otlpResourceSpans `json:"batches"`
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

func (b traceBody) resourceSpans() []otlpResourceSpans {
	switch {
	case b.Trace != nil:
		return b.Trace.ResourceSpans
	case len(b.Batches) > 0:
		return b.Batches
	}
	return b.ResourceSpans
}

// normID turns an id as Tempo wrote it (hex, or base64 of the raw bytes) into lower-case hex of width digits.
func normID(s string, width int) string {
	if s == "" {
		return ""
	}
	if len(s) == width && hexID.MatchString(s) {
		return strings.ToLower(s)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b)*2 == width {
			return fmt.Sprintf("%x", b)
		}
	}
	if hexID.MatchString(s) && len(s) < width {
		return strings.Repeat("0", width-len(s)) + strings.ToLower(s)
	}
	return strings.ToLower(s)
}

func nanos(n json.Number) time.Time {
	v, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil || v <= 0 {
		return time.Time{}
	}
	return time.Unix(0, v).UTC()
}

func spanKind(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	names := map[string]string{"1": "internal", "2": "server", "3": "client", "4": "producer", "5": "consumer"}
	if n, ok := names[s]; ok {
		return n
	}
	return strings.ToLower(strings.TrimPrefix(s, "SPAN_KIND_"))
}

func spanStatus(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	switch s {
	case "1", "STATUS_CODE_OK":
		return "ok"
	case "2", "STATUS_CODE_ERROR":
		return "error"
	}
	return "unset"
}

func strAttr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
