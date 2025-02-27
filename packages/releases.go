// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package packages

const (
	ReleaseExperimental = "experimental"
	ReleaseBeta         = "beta"
	ReleaseGa           = "ga"

	// Default release if no release is configured
	DefaultRelease = ReleaseExperimental
	DefaultLicense = "basic"
)

var ReleaseTypes = map[string]interface{}{
	ReleaseExperimental: nil,
	ReleaseBeta:         nil,
	ReleaseGa:           nil,
}

func IsValidRelease(release string) bool {
	_, exists := ReleaseTypes[release]
	return exists
}
