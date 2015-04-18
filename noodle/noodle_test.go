package noodle

import (
	"testing"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"github.com/bulletproofnetworks/marksman/coco/noodle"
	"time"
	"net/http"
	"io/ioutil"
	"encoding/json"
)

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
		Bind: "0.0.0.0:26080",
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

	// FIXME(lindsay): if there's no sleep, we get a panic. work out why
	time.Sleep(1000 * time.Millisecond)

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
