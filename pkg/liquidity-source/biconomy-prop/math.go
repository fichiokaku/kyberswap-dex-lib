package biconomyprop

import (
	"github.com/holiman/uint256"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/big256"
)

// segmentOut sets out = floor(take * price / 1e18), the exact per-segment
// arithmetic of PropAMMVenue._plan (which itself is
// the executor's _sweepLevels). Everything downstream of this floor matches
// onchain delivery to the wei.
func segmentOut(out, take, price *uint256.Int) {
	big256.MulWadDown(out, take, price)
}

// boardLive replicates PropAMMVenue._segments' inclusion test for one member
// board: mirrored (synced), unexpired, and with depth left past the
// version's lifetime cursor.
func boardLive(b *Board, now uint64) bool {
	n := len(b.Sizes)
	if !b.Synced || n == 0 || len(b.Prices) != n || b.Filled == nil {
		return false
	}
	if now > b.ExpiresAt {
		return false
	}
	return b.Filled.Cmp(b.Sizes[n-1]) < 0
}
