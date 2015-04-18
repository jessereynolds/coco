package noodle

import (
	"log"
	"github.com/go-martini/martini"
	"net/http"
	"expvar"
	"fmt"
	"io/ioutil"
	"strings"
	"strconv"
	"bytes"
	"encoding/json"
	consistent "github.com/stathat/consistent"
	"github.com/bulletproofnetworks/marksman/coco/coco"
)

type ErrorJSON struct {
	Msg		string `json:"error"`
}

func errorJSON(err error) []byte {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "%+v", err)
	errResp := ErrorJSON{Msg: buf.String(),}
	e, _ := json.Marshal(errResp)
	return e
}

func Fetch(fetch coco.FetchConfig, tiers *[]coco.Tier) {
	if len(fetch.Bind) == 0 {
		log.Fatal("fatal: No address configured to bind web server.")
	}

	for i, tier := range *tiers {
		(*tiers)[i].Hash = consistent.New()
		for _, t := range(tier.Targets) {
			(*tiers)[i].Hash.Add(t)
		}
	}

    m := martini.Classic()
	m.Get("/data/:hostname/(.+)", func(params martini.Params, req *http.Request) []byte {
		for _, tier := range *tiers {
			// Lookup the hostname in the tier's hash. Work out where we should proxy to.
			site, err := tier.Hash.Get(params["hostname"])
			if err != nil {
				defer func() { errorCounts.Add("con.get", 1) }()
				return errorJSON(err)
			}

			// Construct the URL, and do the GET
			host := strings.Split(site, ":")[0]
			url := "http://" + host + req.RequestURI
			client := &http.Client{ Timeout: fetch.Timeout.Duration }
			resp, err := client.Get(url)
			if err != nil {
				defer func() { errorCounts.Add("http.get", 1) }()
				return errorJSON(err)
			}
			defer resp.Body.Close()

			// TODO(lindsay): count successful requests to each tier
			// TODO(lindsay): count failed requests to each tier

			// Read the body, check for any errors
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				defer func() { errorCounts.Add("ioutil.readall", 1) }()
				return errorJSON(err)
			}

			// Track metrics for a successful proxy request
			defer func() {
				fetchCounts.Add(site, 1) // the site in the ring we proxied to
				fetchCounts.Add("total", 1)
				respCounts.Add(strconv.Itoa(resp.StatusCode), 1)
				bytesProxied.Add(resp.ContentLength)
			}()

			// return the body
			return body
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

	log.Printf("info: binding web server to %s", fetch.Bind)
	log.Fatal(http.ListenAndServe(fetch.Bind, m))
}

var (
	fetchCounts  = expvar.NewMap("target.requests")
	respCounts   = expvar.NewMap("target.response.codes")
	bytesProxied = expvar.NewInt("bytes.proxied")
	errorCounts	*expvar.Map
)

func init() {
	errors := expvar.Get("errors")
	if errors == nil {
		errorCounts = expvar.NewMap("errors")
	}
}
