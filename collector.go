package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v3"
)

type MBMDCollector struct {
	baseURL       string
	client        *http.Client
	descs         map[string]*prometheus.Desc
	deviceNameMap map[string]string
}

func NewMBMDCollector(baseURL string, timeout time.Duration, yamlPath string) *MBMDCollector {
	m := &MBMDCollector{
		baseURL:       strings.TrimRight(baseURL, "/"),
		client:        &http.Client{Timeout: timeout},
		descs:         make(map[string]*prometheus.Desc),
		deviceNameMap: make(map[string]string),
	}

	if yamlPath == "" {
		yamlPath = "/etc/mbmd.yaml"
	}
	m.deviceNameMap = loadDeviceNameMap(yamlPath)

	gaugeKeys := map[string]string{
		"frequency_hz":            "Frequency (Hz)",
		"power_l1_watts":          "Power L1 (W)",
		"power_l2_watts":          "Power L2 (W)",
		"power_l3_watts":          "Power L3 (W)",
		"power_watts":             "Power (W)",
		"apparent_power_watts":    "Apparent Power (W)",
		"apparent_power_l1_watts": "Apparent Power L1 (W)",
		"apparent_power_l2_watts": "Apparent Power L2 (W)",
		"apparent_power_l3_watts": "Apparent Power L3 (W)",
		"voltage_l1_volts":        "Voltage L1 (V)",
		"voltage_l2_volts":        "Voltage L2 (V)",
		"voltage_l3_volts":        "Voltage L3 (V)",
		"voltage_volts":           "Voltage (V)",
		"current_l1_amps":         "Current L1 (A)",
		"current_l2_amps":         "Current L2 (A)",
		"current_l3_amps":         "Current L3 (A)",
		"current_amps":            "Current (A)",
		"reactive_power_vars":     "Reactive Power (var)",
	}

	counterKeys := map[string]string{
		"total_l1_kwh":  "Total L1 (kWh)",
		"total_l2_kwh":  "Total L2 (kWh)",
		"total_l3_kwh":  "Total L3 (kWh)",
		"total_kwh":     "Total (kWh)",
		"import_l1_kwh": "Import L1 (kWh)",
		"import_l2_kwh": "Import L2 (kWh)",
		"import_l3_kwh": "Import L3 (kWh)",
		"import_kwh":    "Import (kWh)",
		"export_l1_kwh": "Export L1 (kWh)",
		"export_l2_kwh": "Export L2 (kWh)",
		"export_l3_kwh": "Export L3 (kWh)",
		"export_kwh":    "Export (kWh)",
	}

	for k, help := range gaugeKeys {
		m.descs[k] = prometheus.NewDesc(
			"mbmd_"+k,
			help,
			[]string{"device", "name"},
			nil,
		)
	}
	for k, help := range counterKeys {
		m.descs[k] = prometheus.NewDesc(
			"mbmd_"+k,
			help,
			[]string{"device", "name"},
			nil,
		)
	}

	return m
}

func (m *MBMDCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range m.descs {
		ch <- d
	}
}

func (m *MBMDCollector) Collect(ch chan<- prometheus.Metric) {
	url := m.baseURL + "/api/last"
	resp, err := m.client.Get(url)
	if err != nil {
		log.Printf("mbmd fetch failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("mbmd upstream error: status=%d body=%s", resp.StatusCode, string(b))
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("mbmd read body failed: %v", err)
		return
	}

	var data map[string]map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("mbmd json unmarshal failed: %v", err)
		return
	}

	gaugeMap := map[string]string{
		"Frequency":       "frequency_hz",
		"PowerL1":         "power_l1_watts",
		"PowerL2":         "power_l2_watts",
		"PowerL3":         "power_l3_watts",
		"Power":           "power_watts",
		"ApparentPower":   "apparent_power_watts",
		"ApparentPowerL1": "apparent_power_l1_watts",
		"ApparentPowerL2": "apparent_power_l2_watts",
		"ApparentPowerL3": "apparent_power_l3_watts",
		"VoltageL1":       "voltage_l1_volts",
		"VoltageL2":       "voltage_l2_volts",
		"VoltageL3":       "voltage_l3_volts",
		"Voltage":         "voltage_volts",
		"CurrentL1":       "current_l1_amps",
		"CurrentL2":       "current_l2_amps",
		"CurrentL3":       "current_l3_amps",
		"Current":         "current_amps",
		"ReactivePower":   "reactive_power_vars",
	}

	counterMap := map[string]string{
		"SumL1":    "total_l1_kwh",
		"SumL2":    "total_l2_kwh",
		"SumL3":    "total_l3_kwh",
		"Sum":      "total_kwh",
		"ImportL1": "import_l1_kwh",
		"ImportL2": "import_l2_kwh",
		"ImportL3": "import_l3_kwh",
		"Import":   "import_kwh",
		"ExportL1": "export_l1_kwh",
		"ExportL2": "export_l2_kwh",
		"ExportL3": "export_l3_kwh",
		"Export":   "export_kwh",
	}

	for deviceID, values := range data {
		idStr := extractID(deviceID)
		pretty := ""
		if idStr != "" {
			if v, ok := m.deviceNameMap[idStr]; ok {
				pretty = v
			}
		}

		for key, metricName := range gaugeMap {
			v, ok := values[key]
			if !ok {
				continue
			}
			f, ok := ifaceToFloat(v)
			if !ok {
				continue
			}
			if desc, ok := m.descs[metricName]; ok {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, f, deviceID, pretty)
			} else {
				fmt.Printf("unknown desc for %s", metricName)
			}
		}

		for key, metricName := range counterMap {
			v, ok := values[key]
			if !ok {
				continue
			}
			f, ok := ifaceToFloat(v)
			if !ok {
				continue
			}
			if desc, ok := m.descs[metricName]; ok {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, f, deviceID, pretty)
			} else {
				fmt.Printf("unknown desc for %s", metricName)
			}
		}
	}
}

func ifaceToFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		if t == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func extractID(deviceID string) string {
	if deviceID == "" {
		return ""
	}
	parts := strings.Split(deviceID, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

func loadDeviceNameMap(path string) map[string]string {
	out := make(map[string]string)
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}

	var cfg struct {
		Devices []struct {
			Name    string `yaml:"name"`
			Type    string `yaml:"type"`
			Id      int    `yaml:"id"`
			Adapter string `yaml:"adapter"`
		} `yaml:"devices"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		log.Printf("failed to parse %s: %v", path, err)
		return out
	}
	for _, d := range cfg.Devices {
		idStr := strconv.Itoa(d.Id)
		out[idStr] = d.Name
	}
	return out
}
