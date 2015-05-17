package main

import (
	consistent "github.com/stathat/consistent"
	"io/ioutil"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func buildMapping(mapping map[string][]string, hosts []string, con *consistent.Consistent) {
	for _, host := range hosts {
		site, _ := con.Get(host)
		mapping[site] = append(mapping[site], host)
	}
}

func TestRehashingWithManyReplicas(t *testing.T) {
	lines, err := ioutil.ReadFile("hosts.txt")
	if err != nil {
		t.Fatalf("Couldn't read test data: %s", err)
	}
	hosts := strings.Split(string(lines), "\n")

	maxSites := 50
	maxReplicas := 100

	for s := 2; s <= maxSites; s++ {
		for i := 1; i <= maxReplicas; i++ {
			// Initialize the mappings and consistent hasher
			mapping := make(map[string][]string, len(hosts))
			con := consistent.New()
			con.NumberOfReplicas = i

			// Add members to the circle
			for i := 0; i < s; i++ {
				target := string(i)
				//target := strconv.Itoa(i)
				con.Add(target)
			}

			// Build before mapping
			buildMapping(mapping, hosts, con)

			var data []int
			for _, objects := range mapping {
				data = append(data, len(objects))
			}
			sort.Ints(data)
			max := float64(data[len(data)-1])
			min := float64(data[0])
			variance := max / min

			// Print results
			t.Logf("{\"sites\":%d,\"replicas\":%d,\"variance\":%.4f}\n", s, con.NumberOfReplicas, variance)
		}
	}
}
