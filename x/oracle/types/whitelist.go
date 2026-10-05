package types

import (
	"fmt"
	"sort"
	"time"
)

// Liquidity Fabric spec §26.3: "Atraso de ativação de mudança na whitelist do
// oracle: ≥ 7 dias após aprovação", a limit that only a software upgrade may
// change. Both bounds are code constants on purpose: they are not parameters,
// so governance cannot shorten them, and there is no test-only override.
const (
	// WhitelistActivationDelay is the minimum number of blocks between the
	// approval of a whitelist change and its activation: 7 days at the 6 s
	// block target (7 * 24 * 3600 / 6 = 100800).
	WhitelistActivationDelay int64 = 100800

	// WhitelistActivationPeriod is the minimum wall-clock time (block time)
	// between the approval of a whitelist change and its activation, so the
	// 7 days hold even if blocks get faster than the 6 s target.
	WhitelistActivationPeriod = 7 * 24 * time.Hour
)

// NewWhitelistSnapshot returns a snapshot of the given lists approved at
// (height, t) and activating after the §26.3 delay.
func NewWhitelistSnapshot(whitelist DenomList, assets AssetList, height int64, t time.Time) WhitelistSnapshot {
	return WhitelistSnapshot{
		Whitelist:        copyDenomList(whitelist),
		AssetWhitelist:   copyAssetList(assets),
		ApprovedHeight:   height,
		ApprovedTime:     t,
		ActivationHeight: height + WhitelistActivationDelay,
		ActivationTime:   t.Add(WhitelistActivationPeriod),
	}
}

// NewImmediateWhitelistSnapshot returns a snapshot active from (height, t):
// used for the genesis whitelist and for chains upgraded from before §26.3,
// where the lists already in force are not a change.
func NewImmediateWhitelistSnapshot(whitelist DenomList, assets AssetList, height int64, t time.Time) WhitelistSnapshot {
	return WhitelistSnapshot{
		Whitelist:        copyDenomList(whitelist),
		AssetWhitelist:   copyAssetList(assets),
		ApprovedHeight:   height,
		ApprovedTime:     t,
		ActivationHeight: height,
		ActivationTime:   t,
	}
}

// IsDue reports whether the snapshot may become active at (height, t): both
// the block delay and the wall-clock period must have elapsed.
func (s WhitelistSnapshot) IsDue(height int64, t time.Time) bool {
	return height >= s.ActivationHeight && !t.Before(s.ActivationTime)
}

// Matches reports whether the snapshot holds the same lists as the given
// ones. Order is irrelevant (vote targets are a set); duplicates and Tobin
// tax changes are differences.
func (s WhitelistSnapshot) Matches(whitelist DenomList, assets AssetList) bool {
	return SameDenomList(s.Whitelist, whitelist) && SameAssetList(s.AssetWhitelist, assets)
}

// Lists reports whether name is a denom or an asset of the snapshot.
func (s WhitelistSnapshot) Lists(name string) bool {
	for _, d := range s.Whitelist {
		if d.Name == name {
			return true
		}
	}
	return s.AssetWhitelist.Contains(name)
}

// ValidatePending checks a pending snapshot (genesis): valid lists and a
// schedule that honours the §26.3 delay.
func (s WhitelistSnapshot) ValidatePending() error {
	if err := s.validateLists(); err != nil {
		return err
	}
	if s.ActivationHeight-s.ApprovedHeight < WhitelistActivationDelay {
		return fmt.Errorf("pending oracle whitelist activates %d blocks after approval, below the %d-block minimum",
			s.ActivationHeight-s.ApprovedHeight, WhitelistActivationDelay)
	}
	if s.ActivationTime.Before(s.ApprovedTime.Add(WhitelistActivationPeriod)) {
		return fmt.Errorf("pending oracle whitelist activates before the %s minimum after approval", WhitelistActivationPeriod)
	}
	return nil
}

// ValidateActive checks an active snapshot (genesis).
func (s WhitelistSnapshot) ValidateActive() error {
	return s.validateLists()
}

func (s WhitelistSnapshot) validateLists() error {
	if err := validateWhitelist(s.Whitelist); err != nil {
		return err
	}
	if err := validateAssetWhitelist(s.AssetWhitelist); err != nil {
		return err
	}
	for _, d := range s.Whitelist {
		if s.AssetWhitelist.Contains(d.Name) {
			return fmt.Errorf("oracle whitelist snapshot lists %s as both a denom and an asset", d.Name)
		}
	}
	return nil
}

// SameDenomList compares two denom whitelists as multisets of (name, tobin tax).
func SameDenomList(a, b DenomList) bool {
	if len(a) != len(b) {
		return false
	}
	ka, kb := denomKeys(a), denomKeys(b)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}

// SameAssetList compares two asset whitelists as multisets of names.
func SameAssetList(a, b AssetList) bool {
	if len(a) != len(b) {
		return false
	}
	ka, kb := assetKeys(a), assetKeys(b)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}

func denomKeys(l DenomList) []string {
	keys := make([]string, len(l))
	for i, d := range l {
		tax := "<nil>"
		if !d.TobinTax.IsNil() {
			tax = d.TobinTax.String()
		}
		keys[i] = d.Name + "\x00" + tax
	}
	sort.Strings(keys)
	return keys
}

func assetKeys(l AssetList) []string {
	keys := make([]string, len(l))
	for i, a := range l {
		keys[i] = a.Name
	}
	sort.Strings(keys)
	return keys
}

func copyDenomList(l DenomList) DenomList {
	out := make(DenomList, len(l))
	copy(out, l)
	return out
}

func copyAssetList(l AssetList) AssetList {
	out := make(AssetList, len(l))
	copy(out, l)
	return out
}
