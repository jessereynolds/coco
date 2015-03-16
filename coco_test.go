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
	sampleSize    := 1000000
	beforeMapping := make(map[string]string, sampleSize)
	afterMapping  := make(map[string]string, sampleSize)
	con  		  := consistent.New()

	for size := 1 ; size < 1000 ; size++ {
		// Add members to the circle
		for i := 0 ; i < size ; i++ {
			con.Add(string(i))
		}

		// Build before mapping
		buildMapping(beforeMapping, sampleSize, con)

		// Add 20% new members to the circle
		for i := size ; float64(i) < (float64(size) / float64(100) * float64(120)) ; i++ {
			con.Add(string(i))
		}

		// Build after mapping
		buildMapping(afterMapping, sampleSize, con)

		// Determine how many were rehashed
		percentage := rehashed(beforeMapping, afterMapping)

		// Print percentage that were rehashed
		t.Logf("Members count: %d\n", size)
		t.Logf("Percentage rehashed: %f\n", percentage)
	}
}

func TestRehashingWithManyReplicas(t *testing.T) {
	sampleSize    := 1000000
	beforeMapping := make(map[string]string, sampleSize)
	afterMapping  := make(map[string]string, sampleSize)
	con  		  := consistent.New()
	con.NumberOfReplicas = 100

	for size := 1 ; size < 1000 ; size++ {
		// Add members to the circle
		for i := 0 ; i < size ; i++ {
			con.Add(string(i))
		}

		// Build before mapping
		buildMapping(beforeMapping, sampleSize, con)

		// Add 20% new members to the circle
		for i := size ; float64(i) < (float64(size) / float64(100) * float64(120)) ; i++ {
			con.Add(string(i))
		}

		// Build after mapping
		buildMapping(afterMapping, sampleSize, con)

		// Determine how many were rehashed
		percentage := rehashed(beforeMapping, afterMapping)

		// Print percentage that were rehashed
		t.Logf("Members count: %d\n", size)
		t.Logf("Percentage rehashed: %f\n", percentage)
	}
}
