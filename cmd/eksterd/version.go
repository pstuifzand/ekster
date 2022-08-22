package main

import (
	"fmt"
)

var (
	// Version is the version of the build
	Version = "dev"
	// CommitHash is the commit hash from which the build was created
	CommitHash = "n/a"
	// BuildTime contains the time the build was created
	BuildTime = "n/a"
)

// BuildVersion returns a version string for this build
func BuildVersion() string {
	return fmt.Sprintf("%s-%s (%s)", Version, CommitHash, BuildTime)
}
