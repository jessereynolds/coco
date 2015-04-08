package visage

import (
	"net/http"
	"net/url"
	"io/ioutil"
	"encoding/json"
	"strings"
	"time"
	"strconv"
)


/*
func Fetch(endpoint string, base string, rrd string, window time.Duration) ([]float64) {
	host     := base
	plugin   := "curl_json-coco"
	instance := "operations-errors-send-write"
	//instance := "operations-send-" + rrd + ":25826"
	ds       := "value"
*/

type Params struct {
	Endpoint string
	Host	 string
	Plugin	 string
	Instance string
	Ds		 string
	Window	 time.Duration
}

// Fetch queries Visage and returns an array of numerical metrics
func Fetch(params Params) ([]float64) {
	// Construct the path
	parts  := []string{"http:/", params.Endpoint, "data", params.Host, params.Plugin, params.Instance}
	path   := strings.Join(parts, "/")

	// Construct the parameters
	query := url.Values{}
	start  := strconv.Itoa(int(time.Now().Unix() - int64(params.Window.Seconds())))
	query.Add("start", start)

	// Construct the URL
	url := path + "?" + query.Encode()

	// Make the request
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}

	// Read the response
	body, err := ioutil.ReadAll(resp.Body)

	// Map the data into an interface, so we can handle arbitrary data types
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		panic(err)
	}

	// Traverse the returned data structure. This may panic if the returned data
	// doesn't match the plugin/instance format, but we'll catch it with handleErrors()
	series := data[params.Host].
	(map[string]interface{})[params.Plugin].
	(map[string]interface{})[params.Instance].
	(map[string]interface{})

	datas := series[params.Ds].(map[string]interface{})["data"]
	values := datas.([]interface{})

	slice := []float64{}

	// Iterate through all the values, drop ones that aren't float64s
	for _, v := range(values) {
		if vf, ok := v.(float64); ok {
			slice = append(slice, vf)
		}
	}

	return slice
}
