package main

import (
	"testing"
	consistent "github.com/stathat/consistent"
)

func buildMapping(mapping map[string]string, iterations int, con *consistent.Consistent) {
	for i := 0 ; i < iterations ; i++ {
		k := string(i)
		el, _ := con.Get(k)
		mapping[k] = el
	}
}

func rehashed(a map[string]string, b map[string]string) float64 {
	rehashed := 0

	for i := 0 ; i < len(a) ; i++ {
		k := string(i)
		if a[k] != b[k] {
			rehashed++
		}
	}
	percentage := float64(rehashed) / float64(len(a)) * float64(100)
	return percentage
}

/*
	// Remove 20% of members
	for i := 0 ; i < (membersSize / 10 * 2) ; i++ {
		con.Remove(string(i))
	}
*/

func TestRehashing(t *testing.T) {
	objectsSize := 1000000
	sitesSize	:= 1000

	for sites := 1 ; sites < sitesSize ; sites++ {
		// Re-initialize the mappings and consistent hasher
		beforeMapping := make(map[string]string, objectsSize)
		afterMapping  := make(map[string]string, objectsSize)
		con  		  := consistent.New()

		// Add members to the circle
		for i := 0 ; i < sites ; i++ {
			con.Add(string(i))
		}

		// Build before mapping
		buildMapping(beforeMapping, objectsSize, con)

		// Add 50% new members to the circle
		for i := sites ; float64(i) < (float64(sites) / float64(100) * float64(150)) ; i++ {
			con.Add(string(i))
		}

		// Build after mapping
		buildMapping(afterMapping, objectsSize, con)

		// Determine how many were rehashed
		percentage := rehashed(beforeMapping, afterMapping)

		// Print results
		t.Logf("{\"sites\":%d,\"rehashed\":%f,\"replicas\":%d}\n", sites, percentage, con.NumberOfReplicas)
	}
}

func TestRehashingWithManyReplicas(t *testing.T) {
	objectsSize := 1000000
	sitesSize	:= 1000
	numberOfReplicas := 100

	for sites := 1 ; sites < sitesSize ; sites++ {
		// Re-initialize the mappings and consistent hasher
		beforeMapping := make(map[string]string, objectsSize)
		afterMapping  := make(map[string]string, objectsSize)
		con  		  := consistent.New()
		con.NumberOfReplicas = numberOfReplicas

		// Add members to the circle
		for i := 0 ; i < sites ; i++ {
			con.Add(string(i))
		}

		// Build before mapping
		buildMapping(beforeMapping, objectsSize, con)

		// Add 50% new members to the circle
		for i := sites ; float64(i) < (float64(sites) / float64(100) * float64(150)) ; i++ {
			con.Add(string(i))
		}

		// Build after mapping
		buildMapping(afterMapping, objectsSize, con)

		// Determine how many were rehashed
		percentage := rehashed(beforeMapping, afterMapping)

		// Print results
		t.Logf("{\"sites\":%d,\"rehashed\":%f,\"replicas\":%d}\n", sites, percentage, con.NumberOfReplicas)
	}
}
