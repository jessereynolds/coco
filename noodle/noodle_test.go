package noodle

import (
	"testing"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"github.com/bulletproofnetworks/marksman/coco/noodle"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"net"
)

func poll(t *testing.T, address string) {
	for i := 0 ; i < 1000 ; i++ {
		_, err := net.Dial("tcp", address)
		t.Logf("Dial %s attempt %d", address, i)
		if err == nil { break }
		if i == 999 {
			t.Fatalf("Couldn't establish connection to %s", address)
		}
	}
}

/*
Fetch
 - error on start if no tiers provided
 - returns result from tier with highest resolution available
 - progressively falls back on tiers if first doesn't have data
 - provides empty response if no tiers return data
 - provides lookup function
*/
/*
func TestFetch(t *testing.T) {
}
*/

func TestTierLookup(t *testing.T) {
	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind: "127.0.0.1:26080",
		Timeout: *new(coco.Duration), //.UnmarshalText([]byte("1s")),
	}

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{ Targets: []string{"127.0.0.1:25887"} }
	tierConfig["b"] = coco.TierConfig{ Targets: []string{"127.0.0.1:25888"} }
	tierConfig["c"] = coco.TierConfig{ Targets: []string{"127.0.0.1:25889"} }

	// FIXME(lindsay): Refactor this into Tiers() function
	var tiers []coco.Tier
	for k, v := range(tierConfig) {
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

	for k, v := range(tierConfig) {
		if result[k] != v.Targets[0] {
			t.Errorf("Couldn't find tier %s in response: %s", k, string(body))
		}
	}
}

func TestExpvars(t *testing.T) {
	// Setup Fetch
	fetchConfig := coco.FetchConfig{
		Bind: "127.0.0.1:26081",
		Timeout: *new(coco.Duration), //.UnmarshalText([]byte("1s")),
	}

	tierConfig := make(map[string]coco.TierConfig)
	tierConfig["a"] = coco.TierConfig{ Targets: []string{"127.0.0.1:25887"} }

	// FIXME(lindsay): Refactor this into Tiers() function
	var tiers []coco.Tier
	for k, v := range(tierConfig) {
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
