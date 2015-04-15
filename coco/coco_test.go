package coco

import (
	"testing"
	consistent "github.com/stathat/consistent"
	collectd "github.com/kimor79/gollectd"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"strings"
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
		Bind:	"127.0.0.1:25888",
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
	sendConfig := coco.SendConfig{
		Targets: []string{listenConfig.Bind},
	}
	filtered := make(chan collectd.Packet)
	hash := consistent.New()
	servers := map[string]map[string]int64{}
	go coco.Send(sendConfig, filtered, hash, servers)

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
