package main

import "testing"

func TestSmoke(t *testing.T) {
	// Smoke test: package compiles and runs the test runner.
	// Replaced with real CLI tests in later tasks.
	if 1+1 != 2 {
		t.Fatal("arithmetic is broken")
	}
}
