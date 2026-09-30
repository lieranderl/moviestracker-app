package main

import "testing"

func TestTheAppListensOnThePortCloudRunGives(t *testing.T) {
	t.Setenv("PORT", "9000")
	if got := listenAddr(); got != ":9000" {
		t.Fatalf("listenAddr() = %q, want %q", got, ":9000")
	}
}

func TestTheAppListensOn8080WithoutAPort(t *testing.T) {
	t.Setenv("PORT", "")
	if got := listenAddr(); got != ":8080" {
		t.Fatalf("listenAddr() = %q, want %q", got, ":8080")
	}
}
