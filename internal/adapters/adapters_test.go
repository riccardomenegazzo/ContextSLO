package adapters

import (
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestFalcoDecode(t *testing.T) {
	payload := `{"rule":"Terminal shell","output":"CTX-A1B2C3 shell","output_fields":{"proc.name":"sh","proc.pid":42,"k8s.pod.name":"orders","k8s.ns.name":"prod"}}`
	events, err := Decode("falco", strings.NewReader(payload), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Dimension != domain.Process || !events[0].Attributed {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}
func TestCloudTrailRequiresWorkloadCausality(t *testing.T) {
	payload := `{"Records":[{"eventID":"1","eventTime":"2026-01-01T00:00:00Z","eventSource":"sts.amazonaws.com","eventName":"GetCallerIdentity","requestParameters":{"marker":"CTX-A1B2C3"},"userIdentity":{"arn":"arn:aws:iam::1:role/orders"}}]}`
	events, err := Decode("cloudtrail", strings.NewReader(payload), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Dimension != domain.CloudIAM || events[0].Attributed {
		t.Fatalf("expected visible but unattributed cloud event: %#v", events[0])
	}
}
func TestRejectsPayloadWithoutMarker(t *testing.T) {
	_, err := Decode("falco", strings.NewReader(`{"output":"ordinary event"}`), time.Now())
	if err == nil {
		t.Fatal("expected marker error")
	}
}
