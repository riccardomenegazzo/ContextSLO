package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

var markerPattern = regexp.MustCompile(`CTX-[A-Fa-f0-9]{6,32}`)

// Decode converts a vendor payload into the stable ContextSLO observation contract.
func Decode(adapter string, reader io.Reader, receivedAt time.Time) ([]domain.Observation, error) {
	var payload any
	decoder := json.NewDecoder(io.LimitReader(reader, 4<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", adapter, err)
	}
	records := recordsFor(adapter, payload)
	observations := make([]domain.Observation, 0, len(records))
	for _, record := range records {
		flat := map[string]string{}
		flatten("", record, flat)
		marker := findMarker(flat)
		if marker == "" {
			continue
		}
		dimension := inferDimension(adapter, flat)
		occurred := inferTime(flat, receivedAt)
		observation := domain.Observation{ID: eventID(adapter, flat, marker, dimension), Marker: strings.ToUpper(marker), Cluster: first(flat, "cluster", "cluster_name", "resource.attributes.k8s.cluster.name", "k8s.cluster.name"), Source: adapter, Dimension: dimension, OccurredAt: occurred, ReceivedAt: receivedAt, Workload: first(flat, "output_fields.k8s.pod.name", "k8s.pod.name", "resource.attributes.k8s.pod.name", "process.pod.name"), Namespace: first(flat, "output_fields.k8s.ns.name", "k8s.namespace.name", "resource.attributes.k8s.namespace.name", "process.pod.namespace"), ServiceAccount: first(flat, "k8s.serviceaccount.name", "resource.attributes.k8s.serviceaccount.name"), Identity: first(flat, "userIdentity.arn", "useridentity.arn", "identity", "aws.iam.arn", "principal"), Process: first(flat, "output_fields.proc.name", "process.binary", "process.name", "span.attributes.process.executable.name"), PID: parseInt(first(flat, "output_fields.proc.pid", "process.pid", "pid")), Peer: first(flat, "output_fields.fd.sip", "network.peer.address", "peer"), Endpoint: first(flat, "http.route", "span.attributes.http.route", "requestParameters.host", "endpoint"), Evidence: evidence(adapter, flat), Attributes: flat}
		observation.Attributed = inferAttribution(observation, flat)
		observations = append(observations, observation)
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("payload contains no ContextSLO marker")
	}
	return observations, nil
}

func recordsFor(adapter string, payload any) []map[string]any {
	if object, ok := payload.(map[string]any); ok {
		for _, key := range []string{"Records", "records", "resourceSpans", "resourceLogs", "events"} {
			if values, ok := object[key].([]any); ok {
				out := make([]map[string]any, 0, len(values))
				for _, value := range values {
					if item, ok := value.(map[string]any); ok {
						out = append(out, item)
					}
				}
				if len(out) > 0 {
					return out
				}
			}
		}
		return []map[string]any{object}
	}
	if values, ok := payload.([]any); ok {
		out := make([]map[string]any, 0, len(values))
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}
func flatten(prefix string, value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flatten(next, item, out)
		}
	case []any:
		for i, item := range typed {
			flatten(prefix+"."+strconv.Itoa(i), item, out)
		}
	case string:
		out[prefix] = typed
	case json.Number:
		out[prefix] = typed.String()
	case bool:
		out[prefix] = strconv.FormatBool(typed)
	case nil:
	default:
		out[prefix] = fmt.Sprint(typed)
	}
}
func findMarker(flat map[string]string) string {
	for key, value := range flat {
		if strings.Contains(strings.ToLower(key), "marker") {
			if marker := markerPattern.FindString(value); marker != "" {
				return marker
			}
		}
	}
	for _, value := range flat {
		if marker := markerPattern.FindString(value); marker != "" {
			return marker
		}
	}
	return ""
}
func inferDimension(adapter string, flat map[string]string) domain.Dimension {
	if adapter == "cloudtrail" || first(flat, "eventSource", "eventsource") != "" {
		return domain.CloudIAM
	}
	if adapter == "mcp" || contains(flat, "tools/call", "tool_call", "mcp.") {
		return domain.MCP
	}
	if contains(flat, "dns", "domain") {
		return domain.DNS
	}
	if contains(flat, "openat", "file", "fd.name") {
		return domain.Filesystem
	}
	if contains(flat, "connect", "tcp", "network", "socket") {
		return domain.Network
	}
	if contains(flat, "http", "route", "request") {
		return domain.API
	}
	if contains(flat, "proc.name", "process.binary", "process_exec", "execve", "shell") {
		return domain.Process
	}
	if contains(flat, "k8s", "pod", "container") && adapter != "tetragon" {
		return domain.Kubernetes
	}
	return domain.Process
}
func inferAttribution(o domain.Observation, flat map[string]string) bool {
	switch o.Dimension {
	case domain.CloudIAM:
		return o.Identity != "" && (o.Workload != "" || hasAny(flat, "k8s.pod.name", "service.name", "sourceIdentity", "sessionContext.sessionIssuer.arn"))
	case domain.Kubernetes:
		return o.Workload != "" && o.Namespace != ""
	case domain.Process:
		return o.Process != "" || o.PID > 0
	case domain.Network, domain.DNS, domain.API:
		return o.Workload != "" || o.Process != "" || hasAny(flat, "service.name", "resource.attributes.service.name")
	case domain.MCP:
		return o.Identity != "" || o.Workload != "" || o.Process != ""
	default:
		return o.Workload != "" || o.Process != ""
	}
}
func inferTime(flat map[string]string, fallback time.Time) time.Time {
	for _, key := range []string{"eventTime", "time", "timestamp", "observedTimeUnixNano", "startTimeUnixNano"} {
		value := first(flat, key)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed
		}
		if nanos, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Unix(0, nanos)
		}
	}
	return fallback
}
func eventID(adapter string, flat map[string]string, marker string, d domain.Dimension) string {
	explicit := first(flat, "eventID", "event_id", "id", "spanId")
	if explicit != "" {
		return adapter + ":" + explicit
	}
	sum := sha256.Sum256([]byte(adapter + marker + string(d) + evidence(adapter, flat)))
	return adapter + ":" + hex.EncodeToString(sum[:8])
}
func evidence(adapter string, flat map[string]string) string {
	for _, key := range []string{"output", "rule", "eventName", "name", "method", "message", "body.stringValue"} {
		if value := first(flat, key); value != "" {
			return adapter + " · " + truncate(value, 320)
		}
	}
	return adapter + " · normalized event"
}
func first(flat map[string]string, keys ...string) string {
	for _, wanted := range keys {
		for key, value := range flat {
			if strings.EqualFold(key, wanted) || strings.HasSuffix(strings.ToLower(key), "."+strings.ToLower(wanted)) {
				return value
			}
		}
	}
	return ""
}
func contains(flat map[string]string, needles ...string) bool {
	for key, value := range flat {
		joined := strings.ToLower(key + " " + value)
		for _, needle := range needles {
			if strings.Contains(joined, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}
func hasAny(flat map[string]string, keys ...string) bool { return first(flat, keys...) != "" }
func parseInt(value string) int                          { parsed, _ := strconv.Atoi(value); return parsed }
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
