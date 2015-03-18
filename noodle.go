package main

import (
	"log"
	"github.com/go-martini/martini"
	"net/http"
	"github.com/BurntSushi/toml"
	"gopkg.in/alecthomas/kingpin.v1"
	"expvar"
	"fmt"
	"io/ioutil"
	"strings"
	"strconv"
	"time"
	"bytes"
	"encoding/json"
	consistent "github.com/stathat/consistent"
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

func Fetch(fetch fetchConfig) {
	con := consistent.New()

	for _, t := range(fetch.Targets) {
		con.Add(t)
	}

	log.Printf("info: send: hash ring has %d members: %s", len(con.Members()), con.Members())
	if len(con.Members()) < 1 {
		log.Fatal("fatal: The hash ring has no members configured.")
	}
	if len(fetch.Bind) == 0 {
		log.Fatal("fatal: No address configured to bind web server.")
	}
	bytesProxied := expvar.NewInt("bytes.proxied")

    m := martini.Classic()
	m.Get("/data/:hostname/(.+)", func(params martini.Params, req *http.Request) []byte {
		var err error

		// Lookup the hostname in the hash. Work out where we should proxy to.
		site, err := con.Get(params["hostname"])
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
	})
	// Implement expvars.expvarHandler in Martini.
	m.Get("/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, "{")
		first := true
		expvar.Do(func(kv expvar.KeyValue) {
			if !first {
				fmt.Fprintf(w, ",")
			}
			first = false
			fmt.Fprintf(w, "%q: %s", kv.Key, kv.Value)
		})
		fmt.Fprintf(w, "}\n")
	})

	log.Printf("info: binding web server to %s", fetch.Bind)
	log.Fatal(http.ListenAndServe(fetch.Bind, m))
}

type duration struct {
    time.Duration
}

func (d *duration) UnmarshalText(text []byte) error {
    var err error
    d.Duration, err = time.ParseDuration(string(text))
    return err
}

type cocoConfig struct {
	Listen	listenConfig
	Filter	filterConfig
	Send	sendConfig
	Api		apiConfig
	Fetch	fetchConfig
}

type listenConfig struct {
	Bind	string
	Typesdb	string
}

type filterConfig struct {
	Blacklist	string
}

type sendConfig struct {
	Targets	[]string
}

type apiConfig struct {
	Bind	string
}

type fetchConfig struct {
	Targets	[]string
	Bind	string
	Timeout	duration `toml:"proxy_timeout"`
}

var (
	configPath	= kingpin.Arg("config", "Path to coco config").Default("coco.conf").String()
	fetchCounts = expvar.NewMap("target.requests")
	respCounts  = expvar.NewMap("target.response.codes")
	errorCounts = expvar.NewMap("errors")
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	var config cocoConfig
	if _, err := toml.DecodeFile(*configPath, &config); err != nil {
		log.Fatalln("fatal:", err)
		return
	}

	Fetch(config.Fetch)
}
