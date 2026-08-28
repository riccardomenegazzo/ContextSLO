//go:build linux && ebpf

package truthsensor

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:embed context_truth_bpfel.o
var object []byte

type ebpfSensor struct {
	collection *ebpf.Collection
	reader     *ringbuf.Reader
	links      []link.Link
}

func New() (Sensor, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, err
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(object))
	if err != nil {
		return nil, fmt.Errorf("load eBPF spec: %w", err)
	}
	collection, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("load eBPF collection: %w", err)
	}
	attachments := []struct{ category, name, program string }{{"sched", "sched_process_exec", "record_process_exec"}, {"syscalls", "sys_enter_openat", "record_openat"}, {"syscalls", "sys_enter_connect", "record_connect"}, {"syscalls", "sys_enter_sendto", "record_sendto"}}
	sensor := &ebpfSensor{collection: collection}
	for _, item := range attachments {
		program := collection.Programs[item.program]
		if program == nil {
			sensor.Close()
			return nil, fmt.Errorf("program %s missing", item.program)
		}
		attached, err := link.Tracepoint(item.category, item.name, program, nil)
		if err != nil {
			sensor.Close()
			return nil, fmt.Errorf("attach %s: %w", item.name, err)
		}
		sensor.links = append(sensor.links, attached)
	}
	sensor.reader, err = ringbuf.NewReader(collection.Maps["truth_events"])
	if err != nil {
		sensor.Close()
		return nil, err
	}
	return sensor, nil
}
func (s *ebpfSensor) Run(ctx context.Context, sink func(Event) error) error {
	type rawEvent struct {
		TimestampNS          uint64
		PID, ParentPID, Type uint32
		Command              [16]byte
		Detail               [128]byte
	}
	go func() { <-ctx.Done(); _ = s.reader.Close() }()
	for {
		record, err := s.reader.Read()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		var raw rawEvent
		if err = binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &raw); err != nil {
			return err
		}
		event := Event{TimestampNS: raw.TimestampNS, PID: raw.PID, ParentPID: raw.ParentPID, Type: raw.Type, Command: trim(raw.Command[:]), Detail: trim(raw.Detail[:])}
		if err = sink(event); err != nil {
			return err
		}
	}
}
func (s *ebpfSensor) Close() error {
	if s.reader != nil {
		_ = s.reader.Close()
	}
	for _, attached := range s.links {
		_ = attached.Close()
	}
	if s.collection != nil {
		s.collection.Close()
	}
	return nil
}
func trim(value []byte) string {
	if index := bytes.IndexByte(value, 0); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(string(value))
}
