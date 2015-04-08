/*
anomalous_coco_send checks if Coco's send behaviour has changed over a time period.

This check is useful for determining if there has been an increase or decrease
in packets being sent by Coco to a storage node.

anomalous_coco_send uses the Kolmogorov-Smirnov Test to determine if data in a
window is anomalous. You can read more about how the KS test works here:

  http://www.physics.csbsju.edu/stats/KS-test.html

At a high level, the test works like this:

 - Query Visage to get a window of data.
 - Bisect the window into two equal length windows.
 - Sort the data points in each window in ascending order.
 - Find the D-statistic - the maximum vertical deviation between the two series.
 - Test if the D-statistic is greater than the user specified error rate.

Example usage:

  anomalous_coco_send --base ip-10-101-103-42.ap-southeast-2.compute.internal \
					  --rrd 10.101.103.119
					  --endpoint ***REMOVED***
					  --window 5m

Protips:

 - --debug flag will output values derived from the supplied command line
   arguments, including the URL from which data is being fetched.
 - The --window setting specifies how large a window of data should be fetched.
   If you fetch a window of 10m, it will be divided into two 5 minute windows
   when performing the KS test.
 - The --maximum-deviation setting is the main knob you will want to tune. It
   determines how much of a deviation is acceptable for the KS test.
*/
package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"io/ioutil"
	"encoding/json"
	"strings"
	"sort"
	"gopkg.in/alecthomas/kingpin.v1"
	"time"
	"strconv"
	"math"
)

// fetch queries Visage and returns an array of numerical metrics
func fetch(endpoint string, base string, rrd string, window time.Duration) ([]float64) {
	plugin   := "curl_json-coco"
	instance := "operations-send-" + rrd + ":25826"
	ds       := "value"

	// Construct the URL
	parts  := []string{"http:/", endpoint, "data", base , plugin, instance}
	params := url.Values{}
	start  := strconv.Itoa(int(time.Now().Unix() - int64(window.Seconds())))
	params.Add("start", start)

	url := strings.Join(parts, "/") + "?" + params.Encode()

	if *debug {
		fmt.Println("URL:", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	// Map the data into an interface, so we can handle arbitrary data types
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		panic(err)
	}

	// Traverse the returned data structure. This may panic if the returned data
	// doesn't match the plugin/instance format, but we'll catch it with handleErrors()
	series := data[base].
	(map[string]interface{})[plugin].
	(map[string]interface{})[instance].
	(map[string]interface{})

	datas := series[ds].(map[string]interface{})["data"]
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

// handleErrors performs global error handling for unhandled errors
// Based on code from http://blog.denevell.org/golang-panic-recover.html
func handleErrors() {
	if e := recover(); e != nil {
		fmt.Println("UNKNOWN: check error:", e)
		os.Exit(3)
	}
}

// sortWindow sorts a window of data numerically.
func sortWindow(window []float64) ([]float64) {
	sorted := make([]float64, len(window))
	copy(sorted, window)

	// In-place sort
	sort.Float64s(sorted)
	return sorted
}

// bisectAndSortWindow bisects a window of data into two windows, and sorts them.
func bisectAndSortWindow(window []float64) ([]float64, []float64) {
	min := 0
	mid := len(window) / 2
	max := len(window)
	window1 := sortWindow(window[min:mid])
	window2 := sortWindow(window[mid:max])
	return window1, window2
}

// findMaxDeviation finds the maximum deviation between two window.
func findMaxDeviation(window1 []float64, window2 []float64) (float64, int) {
	var maxi int
	max := 0.0
	for i, _ := range window1 {
		diff := math.Abs(window1[i] - window2[i])
		if diff > max {
			max = diff
			maxi = i
		}
	}
	return max, maxi
}

func plot(window1 []float64, window2 []float64, max float64, maxi int) {
	err := exec.Command("which", "gnuplot").Run()

	if err != nil {
		fmt.Println("WARNING: gnuplot not available")
		return
	}

	cmd := exec.Command("gnuplot")
	stdin, err  := cmd.StdinPipe()
	if err != nil {
		fmt.Println("WARNING: plot: couldn't attach to stdin.")
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("WARNING: plot: couldn't attach to stdout.")
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println("WARNING: plot: couldn't run command: %+v.", err)
		return
	}

	fmt.Fprintf(stdin, "set term dumb\n")
	/*
	// Crappy attempt at highlighting anomalous area.
	//circle := window1[maxi] + math.Abs(window1[maxi] - window2[maxi])
	//fmt.Printf("window1: %.2f, window2: %.2f, mid: %.2f\n", window1[maxi], window2[maxi], circle)
	//fmt.Fprintf(stdin, "set object circle at %d, %.2f size 0.5 fc rgb 'gray'\n", maxi, circle)
	*/
	fmt.Fprintf(stdin, "plot '-' using 2 title '' with lines, '-' using 2 title '' with lines\n")
	for i, _ := range window1 {
		fmt.Fprintf(stdin, "\t%d %.2f\n", i, window1[i])
	}
	fmt.Fprintf(stdin, "e\n")
	for i, _ := range window2 {
		fmt.Fprintf(stdin, "\t%d %.2f\n", i, window2[i])
	}
	stdin.Close()

	output, err := ioutil.ReadAll(stdout)
	if err != nil {
		fmt.Println("WARNING: plot: couldn't read stdout: %+v.", err)
		return
	}
	fmt.Println(string(output))
}

var (
	base 		= kingpin.Flag("base", "The host to query metrics from").Required().String()
	rrd         = kingpin.Flag("rrd", "The storage node to test").Required().String()
	endpoint	= kingpin.Flag("endpoint", "Visage endpoint to query").Required().String()
	deviation	= kingpin.Flag("maximum-deviation", "Acceptable deviation for KS test").Default("10.0").Float()
	window		= kingpin.Flag("window", "Window of time to analyse").Default("120s").Duration()
	debug       = kingpin.Flag("debug", "Enable verbose output (default false)").Bool()
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	if *debug {
		fmt.Println("Base:", *base)
		fmt.Println("RRD:", *rrd)
		fmt.Println("Endpoint:", *endpoint)
		fmt.Printf("Maximum deviation: %.1f\n", *deviation)
		fmt.Println("Window:", *window)
		fmt.Println("Debug:", *debug)
	}

	// Global error handling
	defer handleErrors()

	window := fetch(*endpoint, *base, *rrd, *window)
	window1, window2 := bisectAndSortWindow(window)
	max, maxi := findMaxDeviation(window1, window2)

	if *debug {
		fmt.Println("Window 1:")
		fmt.Println(window1)
		fmt.Println("Window 2:")
		fmt.Println(window2)
		fmt.Println("Max, max index:")
		fmt.Println(max, maxi)
	}

	plot(window1, window2, max, maxi)

	if max > *deviation {
		fmt.Printf("CRITICAL: Deviation (%.2f) is greater than maximum allowed (%.2f)\n", max, *deviation)
		os.Exit(2)
	} else {
		fmt.Printf("OK: Deviation (%.2f) is within tolerances (%.2f)\n", max, *deviation)
		os.Exit(0)
	}

}
