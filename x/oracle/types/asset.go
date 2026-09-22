package types

import (
	"strings"

	"gopkg.in/yaml.v2"
)

// String implements fmt.Stringer interface
func (a Asset) String() string {
	out, _ := yaml.Marshal(a)
	return string(out)
}

// Equal implements the equal interface used by the generated Params.Equal
func (a Asset) Equal(a1 *Asset) bool {
	return a1 != nil && a.Name == a1.Name
}

// AssetList is array of Asset (the asset whitelist, spec §21.6 stage 2)
type AssetList []Asset

// String implements fmt.Stringer interface
func (al AssetList) String() (out string) {
	for _, a := range al {
		out += a.String() + "\n"
	}
	return strings.TrimSpace(out)
}

// Contains reports whether the list has an asset with the given name.
func (al AssetList) Contains(name string) bool {
	for _, a := range al {
		if a.Name == name {
			return true
		}
	}
	return false
}
