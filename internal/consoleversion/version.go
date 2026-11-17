// Package consoleversion defines the console/agent compatibility contract.
package consoleversion

import versionutil "k8s.io/apimachinery/pkg/util/version"

// Version is overridden by the release build's -ldflags.
var Version = "0.3.1-dev"

const ChartVersion = "0.3.1"

func Compatible(value string) bool {
	version, err := versionutil.ParseSemantic(value)
	return err == nil && version.Major() == 0 && version.Minor() == 3
}
