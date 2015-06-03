package visage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Params struct {
	Endpoint string
	Host     string
	Plugin   string
	Instance string
	Ds       string
	Window   time.Duration
	Debug    bool
}

func extract(data map[string]interface{}, params Params) (series interface{}, meta map[string]interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			if params.Debug {
				fmt.Printf("Series: %+v\n", data)
			}
			series = nil
			err = errors.New("Series not found in JSON")
		}
	}()

	if val, ok := data["error"]; ok {
		err = errors.New(val.(string))
	} else {
		series = data[params.Host].(map[string]interface{})[params.Plugin].(map[string]interface{})[params.Instance].(map[string]interface{})[params.Ds].(map[string]interface{})["data"]
		if val, ok := data["_meta"]; ok {
			meta = val.(map[string]interface{})
		}
	}

	return series, meta, err
}

// Fetch queries Visage and returns an array of numerical metrics
func Fetch(params Params) ([]float64, error) {
	// Construct the path
	parts := []string{"http:/", params.Endpoint, "data", params.Host, params.Plugin, params.Instance}
	path := strings.Join(parts, "/")

	// Construct the parameters
	query := url.Values{}
	start := strconv.Itoa(int(time.Now().Unix() - int64(params.Window.Seconds())))
	query.Add("start", start)

	// Construct the URL
	url := path + "?" + query.Encode()

	if params.Debug {
		fmt.Printf("URL: %s\n", url)
	}
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
		return make([]float64, 0), err
	}

	series, _, err := extract(data, params)
	if err != nil {
		return make([]float64, 0), err
	}

	values := series.([]interface{})

	slice := []float64{}

	// Iterate through all the values, drop ones that aren't float64s
	for _, v := range values {
		if vf, ok := v.(float64); ok {
			slice = append(slice, vf)
		}
	}

	return slice, nil
}

// Fetch queries Visage and returns an array of numerical metrics
func FetchWithMetadata(params Params) ([]float64, map[string]string, error) {
	// Construct the path
	parts := []string{"http:/", params.Endpoint, "data", params.Host, params.Plugin, params.Instance}
	path := strings.Join(parts, "/")

	// Construct the parameters
	query := url.Values{}
	start := strconv.Itoa(int(time.Now().Unix() - int64(params.Window.Seconds())))
	query.Add("start", start)

	// Construct the URL
	url := path + "?" + query.Encode()

	if params.Debug {
		fmt.Printf("URL: %s\n", url)
	}
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
		return make([]float64, 0), make(map[string]string), err
	}

	series, meta, err := extract(data, params)
	if err != nil {
		return make([]float64, 0), make(map[string]string), err
	}

	// Iterate through all the values, drop ones that aren't float64s
	values := series.([]interface{})
	slice := []float64{}
	for _, v := range values {
		if vf, ok := v.(float64); ok {
			slice = append(slice, vf)
		}
	}

	// Iterate through all the meta keys, converting values to strings
	metadata := map[string]string{}
	for k, v := range meta {
		if vs, ok := v.(string); ok {
			metadata[k] = vs
		}
	}

	return slice, metadata, nil
}
