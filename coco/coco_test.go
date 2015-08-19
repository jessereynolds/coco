package coco

import (
	"encoding/json"
	"github.com/bulletproofnetworks/coco/coco"
	collectd "github.com/kimor79/gollectd"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// MockListener binds to an address listening for UDP datagrams, doing nothing with them
func MockListener(t *testing.T, address string) {
	laddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		t.Fatal("Couldn't resolve address", err)
	}

	_, err = net.ListenUDP("udp", laddr)
	if err != nil {
		t.Fatalf("Couldn't listen to %s: %s", address, err)
	}

	time.Sleep(10 * time.Second)
	return
}

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
	blacklist := make(chan coco.BlacklistItem, 1000)
	go coco.Filter(config, raw, filtered, blacklist)

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
		Plugin:       "irq",
		Type:         "irq",
		TypeInstance: "7",
	}
	name := coco.MetricName(packet)
	expected := 2
	actual := strings.Count(name, "/")
	if actual != expected {
		t.Errorf("Expected %d / separators, got %d", expected, actual)
	}

	packet = collectd.Packet{
		Plugin: "load",
		Type:   "load",
	}
	name = coco.MetricName(packet)
	expected = 1
	actual = strings.Count(name, "/")
	if actual != expected {
		t.Errorf("Expected %d / separators, got %d", expected, actual)
	}
}

func TestTierInitialisation(t *testing.T) {
	// Setup tiers
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:29000", "a.example:29000", "b.example:29000", "c.example:29000"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}
	coco.BuildTiers(&tiers)

	// Test
	for _, tier := range tiers {
		expected := len(tier.Targets)
		actual := len(tier.Hash.Members())
		if actual != expected {
			t.Errorf("Expected %d hash members, got %d\n", expected, actual)
			t.Logf("Connections: %+v\n", tier.Connections)
			t.Logf("Mappings: %+v\n", tier.Mappings)
			t.Logf("Shadows: %+v\n", tier.Shadows)
			t.Logf("Hash.Members(): %+v\n", tier.Hash.Members())
		}
	}
}

func TestSendWhenMissingConnection(t *testing.T) {
	// Setup tiers
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:29000", "a.example:29000", "b.example:29000", "c.example:29000"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	// Launch Api so we can query expvars
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26880",
	}
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)

	poll(t, apiConfig.Bind)

	// Launch Send so we can test dispatch behaviour
	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Query the expvars
	var actual float64
	var expected float64
	var vars map[string]interface{}
	vars = fetchExpvar(t, apiConfig.Bind)
	actual = vars["coco"].(map[string]interface{})["errors"].(map[string]interface{})["send.disconnected"].(float64)
	expected = 0
	if actual != expected {
		t.Fatalf("Expected coco.errors.send.disconnected to be %d, was %d", expected, actual)
	}

	// Dispatch a single packet
	send := collectd.Packet{
		Hostname: "foo",
		Plugin:   "load",
		Type:     "load",
	}
	filtered <- send

	// Check the failure count has increased
	vars = fetchExpvar(t, apiConfig.Bind)
	actual = vars["coco"].(map[string]interface{})["errors"].(map[string]interface{})["send.disconnected"].(float64)
	expected = 1
	if actual != expected {
		t.Fatalf("Expected coco.errors.send.disconnected to be %d, was %d", expected, actual)
	}
}

func fetchExpvar(t *testing.T, bind string) (result map[string]interface{}) {
	resp, err := http.Get("http://" + bind + "/debug/vars")
	if err != nil {
		t.Fatalf("HTTP GET failed: %s", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Errorf("Error when decoding JSON %+v.", err)
		t.Errorf("Response body: %s", string(body))
		t.FailNow()
	}
	return result
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
	go coco.Send(&tiers, filtered)

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

func TestEncodeAbsolute(t *testing.T) {
	// Setup listen
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25963",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet, 500)
	go coco.Listen(listenConfig, raw)

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{listenConfig.Bind}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Dispatch a sample
	value := collectd.Value{
		Name:     "value",
		Type:     uint8(3),
		TypeName: "absolute",
		Value:    5,
	}
	sample := collectd.Packet{
		Hostname: "foo",
		Plugin:   "processes",
		Type:     "absolute",
		Interval: 10,
		Time:     uint64(time.Now().Unix()),
		Values:   []collectd.Value{value},
	}
	filtered <- sample

	// Breathe a moment so packet works its way through
	time.Sleep(100 * time.Millisecond)
	if len(raw) != 1 {
		t.Errorf("Expected %d packets, got %d\n", 1, len(raw))
		t.FailNow()
	}

	// Grab a sample
	s := <-raw
	if len(s.Values) != 1 {
		t.Errorf("Expected %d values in sample, got %d\n", 1, len(s.Values))
		t.FailNow()
	}

	v := s.Values[0]
	t.Logf("before: %+v\n", value)
	t.Logf("after:  %+v\n", v)

	if value.Value != v.Value {
		t.Errorf("Expected value to be %d, got %d\n", value.Value, v.Value)
		t.FailNow()
	}
}

func TestEncodeCounter(t *testing.T) {
	// Setup listen
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25960",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet, 500)
	go coco.Listen(listenConfig, raw)

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{listenConfig.Bind}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Dispatch a sample
	value := collectd.Value{
		Name:     "value",
		Type:     uint8(0),
		TypeName: "counter",
		Value:    161797775,
	}
	sample := collectd.Packet{
		Hostname:       "foo",
		Plugin:         "cpu",
		PluginInstance: "0",
		Type:           "cpu",
		TypeInstance:   "idle",
		Interval:       10,
		Time:           uint64(time.Now().Unix()),
		Values:         []collectd.Value{value},
	}
	filtered <- sample

	// Breathe a moment so packet works its way through
	time.Sleep(100 * time.Millisecond)
	if len(raw) != 1 {
		t.Errorf("Expected %d packets, got %d\n", 1, len(raw))
		t.FailNow()
	}

	// Grab a sample
	s := <-raw
	if len(s.Values) != 1 {
		t.Errorf("Expected %d values in sample, got %d\n", 1, len(s.Values))
		t.FailNow()
	}

	v := s.Values[0]
	t.Logf("before: %+v\n", value)
	t.Logf("after:  %+v\n", v)

	if value.Value != v.Value {
		t.Errorf("Expected value to be %f, got %f\n", value.Value, v.Value)
		t.FailNow()
	}
}

func TestEncodeDerive(t *testing.T) {
	// Setup listen
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25962",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet, 500)
	go coco.Listen(listenConfig, raw)

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{listenConfig.Bind}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Dispatch a sample
	value := collectd.Value{
		Name:     "value",
		Type:     uint8(2),
		TypeName: "derive",
		Value:    1392536,
	}
	sample := collectd.Packet{
		Hostname: "foo",
		Plugin:   "processes",
		Type:     "fork_rate",
		Interval: 10,
		Time:     uint64(time.Now().Unix()),
		Values:   []collectd.Value{value},
	}
	filtered <- sample

	// Breathe a moment so packet works its way through
	time.Sleep(100 * time.Millisecond)
	if len(raw) != 1 {
		t.Errorf("Expected %d packets, got %d\n", 1, len(raw))
		t.FailNow()
	}

	// Grab a sample
	s := <-raw
	if len(s.Values) != 1 {
		t.Errorf("Expected %d values in sample, got %d\n", 1, len(s.Values))
		t.FailNow()
	}

	v := s.Values[0]
	t.Logf("before: %+v\n", value)
	t.Logf("after:  %+v\n", v)

	if value.Value != v.Value {
		t.Errorf("Expected value to be %d, got %d\n", value.Value, v.Value)
		t.FailNow()
	}
}

func TestEncodeGauge(t *testing.T) {
	// Setup listen
	listenConfig := coco.ListenConfig{
		Bind:    "127.0.0.1:25961",
		Typesdb: "../types.db",
	}
	raw := make(chan collectd.Packet, 500)
	go coco.Listen(listenConfig, raw)

	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{listenConfig.Bind}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Dispatch a sample
	value := collectd.Value{
		Name:     "shortterm",
		Type:     uint8(1),
		TypeName: "gauge",
		Value:    0.5,
	}
	sample := collectd.Packet{
		Hostname: "foo",
		Plugin:   "load",
		Type:     "load",
		Interval: 10,
		Time:     uint64(time.Now().Unix()),
		Values:   []collectd.Value{value},
	}
	filtered <- sample

	// Breathe a moment so packet works its way through
	time.Sleep(100 * time.Millisecond)
	if len(raw) != 1 {
		t.Errorf("Expected %d packets, got %d\n", 1, len(raw))
		t.FailNow()
	}

	// Grab a sample
	s := <-raw
	if len(s.Values) != 1 {
		t.Errorf("Expected %d values in sample, got %d\n", 1, len(s.Values))
		t.FailNow()
	}

	v := s.Values[0]
	t.Logf("before: %+v\n", value)
	t.Logf("after:  %+v\n", v)

	if value.Value != v.Value {
		t.Errorf("Expected value to be %d, got %d\n", value.Value, v.Value)
		t.FailNow()
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
	go coco.Send(&tiers, filtered)

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

func TestVirtualReplicasMagic(t *testing.T) {
	// Setup tiers
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25811", "127.0.0.1:25812", "127.0.0.1:25813"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25821", "127.0.0.1:25822", "127.0.0.1:25823"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25831", "127.0.0.1:25832", "127.0.0.1:25833"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	// Setup Send
	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Setup Api
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26840",
	}
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)
	poll(t, apiConfig.Bind)

	// Fetch exposed tiers
	resp, err := http.Get("http://127.0.0.1:26840/tiers")
	if err != nil {
		t.Fatalf("HTTP GET failed: %s", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	//var result []coco.Tier
	var result []map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		t.Errorf("Error when decoding JSON %+v.", err)
		t.Errorf("Response body: %s", string(body))
		t.FailNow()
	}

	for _, tier := range result {
		if tier["virtual_replicas"] == nil {
			t.Errorf("tier '%s' doesn't expose virtual replicas", tier["name"])
		}
		t.Logf("tier '%s' has %.0f virtual replicas", tier["name"], tier["virtual_replicas"])
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
	go coco.Send(&tiers, filtered)

	// Setup API
	apiConfig := coco.ApiConfig{
		Bind: "0.0.0.0:25999",
	}
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)

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

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26080",
	}
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)

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

func TestMeasureQueues(t *testing.T) {
	// Setup Listen
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26081",
	}
	var tiers []coco.Tier
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)

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
	measureConfig.TickInterval.UnmarshalText([]byte("10ms"))
	go coco.Measure(measureConfig, chans, &tiers)

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
			t.Errorf("Expected %s to equal %d, got %.2f", k, expected, v)
		}
	}
}

func TestMeasureDistributionSummaryStats(t *testing.T) {
	// Setup tiers
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25811", "127.0.0.1:25812", "127.0.0.1:25813"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25821", "127.0.0.1:25822", "127.0.0.1:25823"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25831", "127.0.0.1:25832", "127.0.0.1:25833"}}

	for _, v := range tierConfig {
		for _, target := range v.Targets {
			go MockListener(t, target)
		}
	}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	// Setup Api
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26810",
	}
	blacklisted := map[string]map[string]int64{}
	go coco.Api(apiConfig, &tiers, &blacklisted)
	poll(t, apiConfig.Bind)

	// Setup Measure
	chans := map[string]chan collectd.Packet{}
	measureConfig := coco.MeasureConfig{
		TickInterval: *new(coco.Duration),
	}
	measureConfig.TickInterval.UnmarshalText([]byte("100ms"))
	go coco.Measure(measureConfig, chans, &tiers)

	// Setup Send
	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Push packets to Send
	// 1000 hosts
	for i := 0; i < 1000; i++ {
		// up to 24 cpus per host
		iter := rand.Intn(24)
		for n := 0; n < iter; n++ {
			types := []string{"user", "system", "steal", "wait"}
			for _, typ := range types {
				filtered <- collectd.Packet{
					Hostname: "foo" + string(i),
					Plugin:   "cpu-" + strconv.Itoa(n),
					Type:     "cpu-" + typ,
				}
			}
		}
	}

	time.Sleep(10 * time.Millisecond)

	// Fetch exposed expvars
	resp, err := http.Get("http://127.0.0.1:26810/debug/vars")
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

	// Test the exposed expvars data looks sane
	tierProps := result["coco"].(map[string]interface{})["hash.metrics_per_host"].(map[string]interface{})
	t.Logf("Expvar tier props: %+v\n", tierProps)
	if len(tiers) != len(tierProps) {
		t.Errorf("Expected %d tiers to be exposed, got %d\n", len(tiers), len(tierProps))
		//t.Errorf("Tiers: %+v\n", tiers)
		t.Errorf("Exposed tiers: %+v\n", tierProps)
	}
	for _, tier := range tiers {
		targetProps := tierProps[tier.Name].(map[string]interface{})
		if len(targetProps) != len(tier.Targets)+1 {
			t.Errorf("Expected %d targets to be exposed, got %d\n", len(tier.Targets)+1, len(targetProps))
			t.Errorf("Tier targets: %+v\n", tier.Targets)
			t.Errorf("Exposed target properties: %+v\n", targetProps)
		}
		for _, target := range tier.Targets {
			props := targetProps[target].(map[string]interface{})
			for _, k := range []string{"95e", "avg", "max", "min", "sum"} {
				if props[k] == nil {
					t.Errorf("Expected %s metric was not exposed on %s\n", k, target)
				}
			}
		}
	}
}

func TestVariance(t *testing.T) {
	// Setup sender
	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25881", "127.0.0.1:25882", "127.0.0.1:25883", "127.0.0.1:25884", "127.0.0.1:25885", "127.0.0.1:25886", "127.0.0.1:25887", "127.0.0.1:25888"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	filtered := make(chan collectd.Packet)
	go coco.Send(&tiers, filtered)

	// Test dispatch
	for i := 0; i < 100000; i++ {
		send := collectd.Packet{
			Hostname: "foo" + string(i),
			Plugin:   "load",
			Type:     "load",
		}
		filtered <- send
	}

	// Breathe a moment so packet works its way through
	for {
		time.Sleep(10 * time.Millisecond)
		if len(filtered) == 0 {
			break
		}
	}

	for _, tier := range tiers {
		var min float64
		var max float64
		for _, v := range tier.Mappings {
			size := float64(len(v))
			if min == 0 || size < min {
				min = size
			}
			if size > max {
				max = size
			}
		}
		variance := max / min
		maxVariance := 1.2
		t.Logf("Min: %.2f\n", min)
		t.Logf("Max: %.2f\n", max)
		t.Logf("Variance: %.4f\n", variance)
		if variance > maxVariance {
			t.Fatalf("Variance was %.4f, expected < %.4f", variance, maxVariance)
		}
	}
}

func TestBlacklisted(t *testing.T) {
	// Setup Filter
	config := coco.FilterConfig{
		Blacklist: "/(vmem|irq|entropy|users)/",
	}
	raw := make(chan collectd.Packet)
	filtered := make(chan collectd.Packet)
	items := make(chan coco.BlacklistItem, 1000000)
	go coco.Filter(config, raw, filtered, items)

	// Setup blacklist
	blacklisted := map[string]map[string]int64{}
	go coco.Blacklist(items, &blacklisted)

	// Setup Api
	apiConfig := coco.ApiConfig{
		Bind: "127.0.0.1:26082",
	}
	var tiers []coco.Tier
	go coco.Api(apiConfig, &tiers, &blacklisted)
	poll(t, apiConfig.Bind)

	// Push 10 metrics through that should be blacklisted
	for i := 0; i < 10; i++ {
		raw <- collectd.Packet{
			Hostname:     "foo",
			Plugin:       "irq",
			Type:         "irq",
			TypeInstance: strconv.Itoa(i),
		}
	}

	// Fetch blacklisted metrics
	resp, err := http.Get("http://127.0.0.1:26082/blacklisted")
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

	// Test the metrics have been blacklisted
	count := len(result["foo"].(map[string]interface{}))
	expected := 10
	if count != expected {
		t.Errorf("Expected %d blacklisted metrics, got %d", count, expected)
	}
}
