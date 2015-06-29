package ks

import (
	"fmt"
	"io/ioutil"
	"math"
	"os/exec"
	"sort"
)

// SortWindow sorts a window of data numerically.
func SortWindow(window []float64) []float64 {
	sorted := make([]float64, len(window))
	copy(sorted, window)

	// In-place sort
	sort.Float64s(sorted)
	return sorted
}

// BisectAndSortWindow bisects a window of data into two windows, and sorts them.
func BisectAndSortWindow(window []float64) ([]float64, []float64) {
	min := 0
	mid := len(window) / 2
	max := len(window)
	window1 := SortWindow(window[min:mid])
	window2 := SortWindow(window[mid:max])
	return window1, window2
}

// FindMaxDeviation finds the maximum deviation between two window.
func FindMaxDeviation(window1 []float64, window2 []float64) (float64, int) {
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

func Plot(window1 []float64, window2 []float64, max float64, maxi int) {
	err := exec.Command("which", "gnuplot").Run()

	if err != nil {
		fmt.Println("WARNING: gnuplot not available")
		return
	}

	cmd := exec.Command("gnuplot")
	stdin, err := cmd.StdinPipe()
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
