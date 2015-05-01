package coco

import (
	"encoding/json"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	collectd "github.com/kimor79/gollectd"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// poll repeatedly checks if a port is open, exits when it is, fails tests when it doesn't
func poll(t *testing.T, address string) {
	iterations := 1000
	for i := 0; i < iterations; i++ {
		_, err := net.Dial("tcp", address)
		t.Logf("Dial %s attempt %d", address, i)
		if err == nil {
			break
		}
		if i == (iterations - 1) {
			t.Fatalf("Couldn't establish connection to %s", address)
		}
	}
}

/*
Filter
 - Generate metric name
 - increment counter
*/
func TestFilterBlacklistsSamples(t *testing.T) {
	// Setup
	config := coco.FilterConfig{
		Blacklist: "/(vmem|irq|entropy|users)/",
	}
	raw := make(chan collectd.Packet)
	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Filter(config, raw, filtered, servers)

	count := 0
	go func() {
		for {
			<-filtered
			count += 1
		}
	}()

	// Test
	types := []string{"free", "used", "shared", "cached"}
	for _, name := range types {
		raw <- collectd.Packet{
			Hostname:     "foo",
			Plugin:       "memory",
			Type:         "memory",
			TypeInstance: name,
		}
	}
	for i := 0; i < 10; i++ {
		raw <- collectd.Packet{
			Hostname:     "foo",
			Plugin:       "irq",
			Type:         "irq",
			TypeInstance: "7",
		}
	}

	if count != len(types) {
		t.Errorf("Expected %d packets, got %d", len(types), count)
	}
}

// Test that we can generate a metric name
func TestGenerateMetricName(t *testing.T) {
	packet := collectd.Packet{
		Hostname:     "foo",
		Plugin:       "irq",
		Type:         "irq",
		TypeInstance: "7",
	}
	name := coco.MetricName(packet)
	expected := 3
	actual := strings.Count(name, "/")
	if actual != expected {
		t.Errorf("Expected %d / separators, got %d", expected, actual)
	}

	packet = collectd.Packet{
		Hostname: "foo",
		Plugin:   "load",
		Type:     "load",
	}
	name = coco.MetricName(packet)
	expected = 2
	actual = strings.Count(name, "/")
	if actual != expected {
		t.Errorf("Expected %d / separators, got %d", expected, actual)
	}
}

/*
Send
 - Hash lookup
 - Encode a packet
*/
func TestSend(t *testing.T) {
	// Setup listener
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25887",
		Typesdb: "../types.db",
	}
	samples := make(chan collectd.Packet)
	go coco.Listen(listenConfig, samples)

	var receive collectd.Packet
	done := make(chan bool)
	go func() {
		receive = <-samples
		// https://ariejan.net/2014/08/29/synchronize-goroutines-in-your-tests/
		done <- true
	}()

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{listenConfig.Bind}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}
	t.Logf("tiers: %+v\n", tiers)

	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Send(&tiers, filtered, servers)

	// Test dispatch
	send := collectd.Packet{
		Hostname: "foo",
		Plugin:   "load",
		Type:     "load",
	}

	filtered <- send
	<-done

	if send.Hostname != receive.Hostname {
		t.Errorf("Expected %s got %s", send.Hostname, receive.Hostname)
	}
	if send.Plugin != receive.Plugin {
		t.Errorf("Expected %s got %s", send.Plugin, receive.Plugin)
	}
	if send.Type != receive.Type {
		t.Errorf("Expected %s got %s", send.Type, receive.Type)
	}
}

func TestSendTiers(t *testing.T) {
	// Setup listen
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25888",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet)
	go coco.Listen(listenConfig, raw)

	count := 0
	go func() {
		for {
			<-raw
			count += 1
		}
	}()

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Send(&tiers, filtered, servers)

	// Test dispatch
	send := collectd.Packet{
		Hostname: "foo",
		Plugin:   "load",
		Type:     "load",
	}

	filtered <- send

	// Breathe a moment so packet works its way through
	time.Sleep(100 * time.Millisecond)
	if count != len(tierConfig) {
		t.Errorf("Expected %d packets, got %d", len(tierConfig), count)
	}
}

func TestTierLookup(t *testing.T) {
	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25889"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Send(&tiers, filtered, servers)

	// Setup API
	apiConfig := coco.ApiConfig{
		Bind: "0.0.0.0:25999",
	}
	go coco.Api(apiConfig, &tiers, servers)

	poll(t, apiConfig.Bind)

	// Test
	resp, err := http.Get("http://127.0.0.1:25999/lookup?name=abc")
	if err != nil {
		t.Fatalf("HTTP GET failed: %s", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	var result map[string]string
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Fatalf("Error when decoding JSON %+v. Response body: %s", err, string(body))
	}

	for k, v := range tierConfig {
		if result[k] != v.Targets[0] {
			t.Errorf("Couldn't find tier %s in response: %s", k, string(body))
		}
	}
}

func TestExpvars(t *testing.T) {
	// Setup API
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}

	// FIXME(lindsay): Refactor this into Tiers() function
	// tiers := tierConfig.Tiers()
	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26080",
	}
	servers := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, servers)

	poll(t, apiConfig.Bind)

	// Test
	resp, err := http.Get("http://127.0.0.1:26080/debug/vars")
	if err != nil {
		t.Fatalf("HTTP GET failed: %s", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Errorf("Error when decoding JSON %+v.", err)
		t.Errorf("Response body: %s", string(body))
		t.FailNow()
	}

	if result["cmdline"] == nil {
		t.Errorf("Couldn't find 'cmdline' key in JSON.")
		t.Errorf("JSON object: %+v", result)
		t.FailNow()
	}
}

func TestMeasure(t *testing.T) {
	// Setup Listen
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26081",
	}
	var tiers []coco.Tier
	servers := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, servers)

	poll(t, apiConfig.Bind)

	// Setup Measure
	chans := map[string]chan collectd.Packet{
		"a": make(chan collectd.Packet, 1000),
		"b": make(chan collectd.Packet, 1000),
		"c": make(chan collectd.Packet, 1000),
	}
	measureConfig := coco.MeasureConfig{
		TickInterval: *new(coco.Duration),
	}
	measureConfig.TickInterval.UnmarshalText([]byte("1ms"))
	go coco.Measure(measureConfig, chans)

	// Test pushing packets
	for _, c := range chans {
		for i := 0; i < 950; i++ {
			c <- collectd.Packet{}
		}
	}

	time.Sleep(10 * time.Millisecond)

	// Test
	resp, err := http.Get("http://127.0.0.1:26081/debug/vars")
	if err != nil {
		t.Fatalf("HTTP GET failed: %s", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Errorf("Error when decoding JSON %+v.", err)
		t.Errorf("Response body: %s", string(body))
		t.FailNow()
	}

	counts := result["coco"].(map[string]interface{})["queues"].(map[string]interface{})
	expected := 950
	for k, v := range counts {
		c := int(v.(float64))
		if c != expected {
			t.Errorf("Expected %s to equal %d, got %d", k, expected, v)
		}
	}
}
