//go:build !linux

package main

// useClientSideTitlebar is only needed on Linux (see titlebar_linux.go).
func useClientSideTitlebar() {}
