package panel

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Overview adalah data dashboard yang diambil dari /proc dan statfs.
type Overview struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotal    uint64  `json:"mem_total"`
	MemUsed     uint64  `json:"mem_used"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	DiskTotal   uint64  `json:"disk_total"`
	DiskUsed    uint64  `json:"disk_used"`
	Uptime      uint64  `json:"uptime"`
	NumCPU      int     `json:"num_cpu"`
	EngineState string  `json:"engine_state"`
	PanelUptime uint64  `json:"panel_uptime"`
	ServerTime  string  `json:"server_time"`
}

type SystemSampler struct {
	mu         sync.Mutex
	dataDir    string
	started    time.Time
	lastTotal  uint64
	lastIdle   uint64
	lastSample time.Time
}

func NewSystemSampler(dataDir string) *SystemSampler {
	return &SystemSampler{dataDir: dataDir, started: time.Now()}
}

func (s *SystemSampler) Overview() Overview {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := Overview{
		NumCPU:      runtimeNumCPU(),
		EngineState: "not_configured",
		PanelUptime: uint64(time.Since(s.started).Seconds()),
		ServerTime:  time.Now().Format(time.RFC3339),
	}
	if total, idle, ok := readCPUStat(); ok {
		if !s.lastSample.IsZero() && total > s.lastTotal {
			totalDelta := float64(total - s.lastTotal)
			idleDelta := float64(idle - s.lastIdle)
			out.CPUPercent = clampPercent(100 * (totalDelta - idleDelta) / totalDelta)
		}
		s.lastTotal, s.lastIdle = total, idle
		s.lastSample = time.Now()
	}
	if mem, ok := readMeminfo(); ok {
		out.MemTotal = mem["MemTotal"]
		avail := mem["MemAvailable"]
		if avail == 0 {
			avail = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
		}
		if out.MemTotal > avail {
			out.MemUsed = out.MemTotal - avail
		}
		out.SwapTotal = mem["SwapTotal"]
		if mem["SwapTotal"] > mem["SwapFree"] {
			out.SwapUsed = mem["SwapTotal"] - mem["SwapFree"]
		}
	}
	if load, ok := readLoadavg(); ok {
		out.Load1, out.Load5, out.Load15 = load[0], load[1], load[2]
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.dataDir, &stat); err == nil {
		out.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		out.DiskUsed = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	}
	if up, ok := readUptime(); ok {
		out.Uptime = up
	}
	return out
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func runtimeNumCPU() int {
	// runtime.NumCPU dapat berubah dengan GOMAXPROCS; jumlah CPU host lebih
	// relevan untuk dashboard.
	return numCPUFromProc()
}

func numCPUFromProc() int {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 1
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func readCPUStat() (total, idle uint64, ok bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		for i, field := range fields {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			total += value
			if i == 3 || i == 4 {
				idle += value
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

func readMeminfo() (map[string]uint64, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, false
	}
	out := make(map[string]uint64, 16)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out[key] = value * 1024
	}
	return out, true
}

func readLoadavg() ([3]float64, bool) {
	var out [3]float64
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return out, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return out, false
		}
		out[i] = value
	}
	return out, true
}

func readUptime() (uint64, bool) {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return uint64(value), true
}
