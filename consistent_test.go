package main

import (
	consistent "github.com/stathat/consistent"
	"sort"
	"strconv"
	"testing"
)

func buildMapping(mapping map[string][]string, iterations int, con *consistent.Consistent) {
	for i := 0; i < iterations; i++ {
		k := string(i)
		site, _ := con.Get(k)
		mapping[site] = append(mapping[site], k)
	}
}

func TestRehashingWithManyReplicas(t *testing.T) {
	objectsSize := 1000000
	maxSites := 50
	maxReplicas := 100

	for s := 2; s <= maxSites; s++ {
		for i := 1; i <= maxReplicas; i++ {
			// Initialize the mappings and consistent hasher
			mapping := make(map[string][]string, objectsSize)
			con := consistent.New()
			con.NumberOfReplicas = i

			// Add members to the circle
			for i := 0; i < s; i++ {
				con.Add(strconv.Itoa(i))
			}

			// Build before mapping
			buildMapping(mapping, objectsSize, con)

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
