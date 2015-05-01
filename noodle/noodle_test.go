package noodle

import (
	"encoding/json"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"github.com/bulletproofnetworks/marksman/coco/noodle"
	"github.com/bulletproofnetworks/marksman/coco/visage"
	"github.com/go-martini/martini"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"
)

func MockVisage() {
	m := martini.Classic()
	m.Get("/data/:hostname/:plugin/:instance", func(params martini.Params, req *http.Request) []byte {
		var resp interface{}
		resp = map[string]interface{}{
			params["hostname"]: map[string]interface{}{
				params["plugin"]: map[string]interface{}{
					params["instance"]: map[string]interface{}{
						"value": map[string]interface{}{
							"data": make([]float64, 360),
						},
					},
				},
			},
		}
		b, err := json.Marshal(resp)
		if err != nil {
			panic("mockvisage: couldn't encode json")
		}
		return b
	})
	http.ListenAndServe("127.0.0.1:29292", m)
}

func poll(t *testing.T, address string) {
	for i := 0; i < 1000; i++ {
		_, err := net.Dial("tcp", address)
		t.Logf("Dial %s attempt %d", address, i)
		if err == nil {
			break
		}
		if i == 999 {
			t.Fatalf("Couldn't establish connection to %s", address)
		}
	}
}

/*
Fetch
 - returns result from tier with highest resolution available
 - progressively falls back on tiers if first doesn't have data
 - provides empty response if no tiers return data
*/

// Test data can be fetched from noodle
func TestFetch(t *testing.T) {
	go MockVisage()

	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind:         "127.0.0.1:26082",
		ProxyTimeout: *new(coco.Duration),
		RemotePort:   "29292",
	}
	fetchConfig.ProxyTimeout.UnmarshalText([]byte("3s"))

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25889"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	go noodle.Fetch(fetchConfig, &tiers)

	poll(t, fetchConfig.Bind)

	// Test
	params := visage.Params{
		Endpoint: fetchConfig.Bind,
		Host:     "highest",
		Plugin:   "load",
		Instance: "load",
		Ds:       "value",
		Window:   3 * time.Hour,
	}

	window, err := visage.Fetch(params)
	if err != nil {
		t.Fatalf("Error when fetching Visage data: %s\n", err)
	}

	for i, v := range window {
		if v != 0.0 {
			t.Errorf("Unexpected value: expected %f got %f at %d", 0.0, v, i)
		}
	}
}

// Test a bad fetch results in an error
func TestFetchWithFailure(t *testing.T) {
	go MockVisage()

	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind:         "127.0.0.1:26083",
		ProxyTimeout: *new(coco.Duration),
		RemotePort:   "29293",
	}
	fetchConfig.ProxyTimeout.UnmarshalText([]byte("3s"))

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25889"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	go noodle.Fetch(fetchConfig, &tiers)

	poll(t, fetchConfig.Bind)

	// Test
	params := visage.Params{
		Endpoint: fetchConfig.Bind,
		Host:     "highest",
		Plugin:   "load",
		Instance: "load",
		Ds:       "value",
		Window:   3 * time.Hour,
	}

	body, err := visage.Fetch(params)
	if err == nil {
		t.Fatalf("Expected error when fetching Visage data, got: %+v\n", body)
	}
}

// Test the lookup function for determining where a metric is stored
func TestTierLookup(t *testing.T) {
	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind:         "127.0.0.1:26080",
		ProxyTimeout: *new(coco.Duration),
	}
	fetchConfig.ProxyTimeout.UnmarshalText([]byte("3s"))

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}
	tierConfig["b"] = coco.TierConfig{Targets: []string{"127.0.0.1:25888"}}
	tierConfig["c"] = coco.TierConfig{Targets: []string{"127.0.0.1:25889"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	go noodle.Fetch(fetchConfig, &tiers)

	poll(t, fetchConfig.Bind)

	// Test
	resp, err := http.Get("http://127.0.0.1:26080/lookup?name=abc")
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

// Test exposing of expvars
func TestExpvars(t *testing.T) {
	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind:         "127.0.0.1:26081",
		ProxyTimeout: *new(coco.Duration),
	}
	fetchConfig.ProxyTimeout.UnmarshalText([]byte("3s"))

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{Targets: []string{"127.0.0.1:25887"}}

	var tiers []coco.Tier
	for k, v := range tierConfig {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	go noodle.Fetch(fetchConfig, &tiers)

	poll(t, fetchConfig.Bind)

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

	if result["cmdline"] == nil {
		t.Errorf("Couldn't find 'cmdline' key in JSON.")
		t.Errorf("JSON object: %+v", result)
		t.FailNow()
	}
}
