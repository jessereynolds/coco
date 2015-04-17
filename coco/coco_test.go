package coco

import (
	"testing"
	collectd "github.com/kimor79/gollectd"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"strings"
	"time"
	"net/http"
	"io/ioutil"
	"encoding/json"
)

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
			<- filtered
			count += 1
		}
	}()

	// Test
	types := []string{"free", "used", "shared", "cached"}
	for _, name := range(types) {
		raw <- collectd.Packet{
			Hostname: "foo",
			Plugin: "memory",
			Type: "memory",
			TypeInstance: name,
		}
	}
	for i := 0 ; i < 10 ; i++ {
		raw <- collectd.Packet{
			Hostname: "foo",
			Plugin: "irq",
			Type: "irq",
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
		Hostname: "foo",
		Plugin: "irq",
		Type: "irq",
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
		Plugin: "load",
		Type: "load",
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
		Bind:	"127.0.0.1:25887",
		Typesdb: "../types.db",
	}
	samples := make(chan collectd.Packet)
	go coco.Listen(listenConfig, samples)

	var receive collectd.Packet
	done := make(chan bool)
	go func() {
		receive = <- samples
		// https://ariejan.net/2014/08/29/synchronize-goroutines-in-your-tests/
		done <- true
	}()

	// Setup sender
	sendConfig := make(map[string]coco.SendConfig)
	sendConfig["a"] = coco.SendConfig{ Targets: []string{listenConfig.Bind} }

	var tiers []coco.Tier
	for k, v := range(sendConfig) {
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
		Plugin: "load",
		Type: "load",
	}

	filtered <- send
	<- done

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
		Bind:	"127.0.0.1:25888",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet)
	go coco.Listen(listenConfig, raw)

	count := 0
	go func() {
		for {
			<- raw
			count += 1
		}
	}()

	// Setup sender
	sendConfig := make(map[string]coco.SendConfig)
	sendConfig["a"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25888"} }
	sendConfig["b"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25888"} }
	sendConfig["c"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25888"} }

	var tiers []coco.Tier
	for k, v := range(sendConfig) {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Send(&tiers, filtered, servers)

	// Test dispatch
	send := collectd.Packet{
		Hostname: "foo",
		Plugin: "load",
		Type: "load",
	}

	filtered <- send

	time.Sleep(100 * time.Millisecond)
	if count != len(sendConfig) {
		t.Errorf("Expected %d packets, got %d", len(sendConfig), count)
	}
}

func TestApiLookup(t *testing.T) {
	// Setup sender
	sendConfig := make(map[string]coco.SendConfig)
	sendConfig["a"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25887"} }
	sendConfig["b"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25888"} }
	sendConfig["c"] = coco.SendConfig{ Targets: []string{"127.0.0.1:25889"} }

	var tiers []coco.Tier
	for k, v := range(sendConfig) {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	servers := map[string]map[string]int64{}
	go coco.Send(&tiers, filtered, servers)

	// FIXME(lindsay): if there's no sleep, we get a panic. work out why
	time.Sleep(100 * time.Millisecond)

	// Setup API
	apiConfig := coco.ApiConfig{
		Bind: "0.0.0.0:25999",
	}
	go coco.Api(apiConfig, &tiers, servers)

	// Test
	resp, err := http.Get("http://127.0.0.1:25999/lookup?name=abc")
	if err != nil {
		t.Errorf("HTTP GET failed: %s", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	var result map[string]string
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Fatalf("Error when decoding JSON %+v. Response body: %s", err, string(body))
	}

	for k, v := range(sendConfig) {
		t.Logf("%s: %s\n", result[k], v.Targets[0])
		if result[k] != v.Targets[0] {
			t.Errorf("Couldn't find tier %s in response: %s", k, string(body))
		}
	}
}
