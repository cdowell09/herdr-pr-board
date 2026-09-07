package config

import "reflect"

// SameDiscovery compares settings that define a shared discovery observation.
func SameDiscovery(left, right Config) bool {
	return reflect.DeepEqual(left.GitHub, right.GitHub) && reflect.DeepEqual(left.Views, right.Views)
}
