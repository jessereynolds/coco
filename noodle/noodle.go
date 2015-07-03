package noodle

import (
	"bytes"
	"encoding/json"
	"expvar"
	"fmt"
	"github.com/bulletproofnetworks/coco/coco"
	"github.com/go-martini/martini"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type ErrorJSON struct {
	Msg string `json:"error"`
}

func errorJSON(err error) []byte {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "%+v", err)
	errResp := ErrorJSON{Msg: buf.String()}
	e, _ := json.Marshal(errResp)
	return e
}

func Fetch(fetch coco.FetchConfig, tiers *[]coco.Tier) {
	// Initialise the error counts
	errorCounts.Add("fetch.con.get", 0)
	errorCounts.Add("fetch.http.get", 0)
	errorCounts.Add("fetch.ioutil.readall", 0)

	if len(fetch.Bind) == 0 {
		log.Fatal("[fatal] Fetch: No address configured to bind web server.")
	}

	coco.BuildTiers(tiers)

	m := martini.Classic()
	m.Get("/data/:hostname/(.+)", func(params martini.Params, req *http.Request) []byte {
		for _, tier := range *tiers {
			// Lookup the hostname in the tier's hash. Work out where we should proxy to.
			target, err := tier.Lookup(params["hostname"])
			if err != nil {
				log.Printf("[info] Fetch: couldn't lookup target: %s\n", err)
				defer func() { errorCounts.Add("fetch.con.get", 1) }()
				return errorJSON(err)
			}

			// Construct the URL, and do the GET
			var host string
			if len(fetch.RemotePort) > 0 {
				// FIXME(lindsay) look up fetch port per-target?
				host = strings.Split(target, ":")[0] + ":" + fetch.RemotePort
			} else {
				host = strings.Split(target, ":")[0]
			}
			url := "http://" + host + req.RequestURI
			client := &http.Client{Timeout: fetch.Timeout()}
			resp, err := client.Get(url)
			defer resp.Body.Close()
			if err != nil {
				log.Printf("[info] Fetch: couldn't perform GET to target: %s\n", err)
				defer func() { errorCounts.Add("fetch.http.get", 1) }()
				return errorJSON(err)
			}

			// TODO(lindsay): count successful requests to each tier
			// TODO(lindsay): count failed requests to each tier

			// Read the body, check for any errors
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Printf("[info] Fetch: couldn't read response from target: %s\n", err)
				defer func() { errorCounts.Add("fetch.ioutil.readall", 1) }()
				return errorJSON(err)
			}

			// Stuff in metadata about the proxied request
			meta := map[string]string{
				"host":   host,
				"target": target,
				"url":    url,
			}
			var data map[string]interface{}
			err = json.Unmarshal(body, &data)
			if err != nil {
				log.Printf("[info] Fetch: couldn't unmarshal JSON from target: %s\n", err)
				defer func() { errorCounts.Add("fetch.json.unmarshal", 1) }()
				return errorJSON(err)
			}
			data["_meta"] = meta
			bm, err := json.Marshal(data)
			if err != nil {
				log.Printf("[info] Fetch: couldn't re-marshal target JSON for client: %s\n", err)
				defer func() { errorCounts.Add("fetch.json.marshal", 1) }()
				return errorJSON(err)
			}

			// Track metrics for a successful proxy request
			defer func() {
				reqCounts.Add(target, 1) // the target in the hash we proxied to
				reqCounts.Add("total", 1)
				respCounts.Add(strconv.Itoa(resp.StatusCode), 1)
				bytesProxied.Add(resp.ContentLength)
				tierCounts.Add(tier.Name, 1)
			}()

			// return the body with metadata
			return bm
		}

		// TODO(lindsay): Provide a fallback response if there is no data available
		// return the body
		return []byte("oops")
	})
	// Implement expvars.expvarHandler in Martini.
	m.Get("/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		coco.ExpvarHandler(w, r)
	})
	m.Get("/lookup", func(params martini.Params, req *http.Request) []byte {
		return coco.TierLookup(params, req, tiers)
	})

	log.Printf("[info] Fetch: binding web server to %s", fetch.Bind)
	log.Fatalf("[fatal] Fetch: HTTP handler crashed: %s", http.ListenAndServe(config.Bind, m))
}

var (
	tierCounts   = expvar.NewMap("noodle.fetch.tier.requests")
	reqCounts    = expvar.NewMap("noodle.fetch.target.requests")
	respCounts   = expvar.NewMap("noodle.fetch.target.response.codes")
	bytesProxied = expvar.NewInt("noodle.fetch.bytes.proxied")
	errorCounts  = expvar.NewMap("noodle.errors")
)
