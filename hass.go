package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type hassLookup struct {
	Unit       string `json:"unit,omitempty"`
	StateClass string `json:"state_class,omitempty"`
	DevClass   string `json:"device_class,omitempty"`
}

func startHassDiscovery(mqttHost, mqttTopic string, cleanSession, resumeSubs bool, connectTimeout, maxReconnect time.Duration, url string) error {
	lookup := map[string]hassLookup{
		"Export":        {Unit: "kWh", StateClass: "total_increasing", DevClass: "energy"},
		"Import":        {Unit: "kWh", StateClass: "total_increasing", DevClass: "energy"},
		"Sum":           {Unit: "kWh", StateClass: "total_increasing", DevClass: "energy"},
		"Cosphi":        {StateClass: "measurement", DevClass: "power_factor"},
		"Current":       {Unit: "A", StateClass: "measurement", DevClass: "current"},
		"Voltage":       {Unit: "V", StateClass: "measurement", DevClass: "voltage"},
		"Power":         {Unit: "W", StateClass: "measurement", DevClass: "power"},
		"ReactivePower": {Unit: "var", StateClass: "measurement", DevClass: "reactive_power"},
		"Frequency":     {Unit: "Hz", StateClass: "measurement", DevClass: "frequency"},
	}

	opts := mqtt.NewClientOptions().AddBroker(mqttHost)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(maxReconnect)
	opts.SetConnectTimeout(connectTimeout)
	opts.SetCleanSession(cleanSession)
	opts.SetResumeSubs(resumeSubs)

	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		logger.Printf("mqtt connection lost: %v", err)
	})
	opts.SetReconnectingHandler(func(c mqtt.Client, o *mqtt.ClientOptions) {
		logger.Printf("mqtt reconnecting...")
	})

	discoveryCache := make(map[string][]byte)
	var dcMu sync.Mutex

	handler := func(c mqtt.Client, m mqtt.Message) {
		topic := m.Topic()
		parts := strings.Split(topic, "/")
		if len(parts) < 4 {
			return
		}
		device := parts[1]
		kind := parts[2]
		phase := parts[3]
		_other := ""
		if len(parts) > 4 {
			_other = strings.Join(parts[4:], "/")
		}
		if _other != "" {
			logger.Printf("unsupported message (extra path): topic=%s payload=%s", topic, string(m.Payload()))
			return
		}
		if device == "status" {
			return
		}
		if matched, _ := regexp.MatchString(`^T\d+$`, phase); matched {
			return
		}
		lu, ok := lookup[kind]
		if !ok {
			return
		}

		name := fmt.Sprintf("%s %s", kind, func() string {
			if phase == "" {
				return "Total"
			}
			return phase
		}())
		uniqId := fmt.Sprintf("mbmd-exporter-bridge_%s_%s", strings.ReplaceAll(device, ".", "-"), strings.ToLower(strings.ReplaceAll(strings.Trim(strings.Join([]string{kind, phase}, "-"), "-"), ".", "-")))

		data := map[string]interface{}{
			"name":     name,
			"stat_t":   topic,
			"uniq_id":  uniqId,
			"dev_cla":  lu.DevClass,
			"stat_cla": lu.StateClass,
			"exp_aft":  60,
			"dev": map[string]interface{}{
				"name": device,
				"ids":  strings.ReplaceAll(device, ".", "-"),
				"cu":   url,
				"mf":   "mbmd",
				"mdl":  device,
			},
		}
		if lu.Unit != "" {
			data["unit_of_meas"] = lu.Unit
		}

		payload, _ := json.Marshal(data)
		configTopic := fmt.Sprintf("homeassistant/sensor/mbmd-exporter-bridge-%s/%s/config", strings.ReplaceAll(device, ".", "-"), strings.ToLower(strings.ReplaceAll(strings.Trim(strings.Join([]string{kind, phase}, "-"), "-"), ".", "-")))

		tok := c.Publish(configTopic, 0, true, payload)
		tok.Wait()
		if tok.Error() != nil {
			logger.Printf("failed to publish discovery: %v", tok.Error())
		}

		dcMu.Lock()
		discoveryCache[configTopic] = payload
		dcMu.Unlock()
	}

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logger.Printf("mqtt connected: (re)subscribing to %s", mqttTopic)
		if token := c.Subscribe(mqttTopic, 0, handler); token.Wait() && token.Error() != nil {
			logger.Printf("subscribe failed in OnConnect: %v", token.Error())
		}
		dcMu.Lock()
		for k, v := range discoveryCache {
			if tok := c.Publish(k, 0, true, v); tok.Wait() && tok.Error() != nil {
				logger.Printf("republish discovery failed: %v", tok.Error())
			}
		}
		dcMu.Unlock()
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect mqtt: %v", token.Error())
	}

	logger.Printf("started MQTT Home Assistant discovery on %s", mqttHost)
	return nil
}
