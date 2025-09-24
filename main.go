package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var logger = log.New(os.Stderr, "", log.LstdFlags)

func main() {
	listenAddr := flag.String("listen", ":8080", "listen address, e.g. :8080 or 0.0.0.0:8080")
	url := flag.String("url", "", "mbmd base URL (e.g. http://192.168.1.10:8080)")
	hassEnable := flag.Bool("hass-enable", false, "enable Home Assistant MQTT discovery/publishing")
	mqttHost := flag.String("mqtt-host", "", "mqtt broker URI (e.g. tcp://mqtt:1883)")
	mqttTopic := flag.String("mqtt-topic", "mbmd/#", "mqtt topic to subscribe to (default: mbmd/#)")
	cleanSession := flag.Bool("clean-session", true, "MQTT CleanSession option (true = do not persist server-side subs)")
	resumeSubs := flag.Bool("resume-subs", false, "Use client-side resume subs (SetResumeSubs)")
	connectTimeout := flag.Duration("mqtt-connect-timeout", 30*time.Second, "MQTT connect timeout")
	maxReconnect := flag.Duration("mqtt-max-reconnect", 2*time.Minute, "MQTT max reconnect interval")
	mbmdYaml := flag.String("mbmd-yaml", "", "optional path to mbmd.yaml for device name mapping (default /etc/mbmd.yaml)")
	flag.Parse()

	if *url == "" {
		logger.Fatalf("--url is required")
	}
	if *hassEnable && *mqttHost == "" {
		logger.Fatalf("--hass-enable set but --mqtt-host is empty")
	}

	if *hassEnable {
		go startHassDiscovery(*mqttHost, *mqttTopic, *cleanSession, *resumeSubs, *connectTimeout, *maxReconnect, *url)
	}

	collector := NewMBMDCollector(*url, 5*time.Second, *mbmdYaml)
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	http.Handle("/metrics", handler)

	logger.Printf("listening on %s", *listenAddr)
	logger.Fatal(http.ListenAndServe(*listenAddr, nil))
}
