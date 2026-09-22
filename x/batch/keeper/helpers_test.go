package keeper_test

import "cosmossdk.io/collections"

func collectionsJoin(a uint64, b string) collections.Pair[uint64, string] {
	return collections.Join(a, b)
}

func collectionsJoin3(a uint64, b, c string) collections.Triple[uint64, string, string] {
	return collections.Join3(a, b, c)
}
