//go:build !linux || !ebpf

package truthsensor

import "fmt"

func New() (Sensor, error) {
	return nil, fmt.Errorf("eBPF sensor is not included in this build; generate the object and build with -tags ebpf on Linux")
}
