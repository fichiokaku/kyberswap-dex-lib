package biconomyprop

import (
	"github.com/holiman/uint256"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/big256"
)

// segmentOut sets out = floor(take * price / 1e18), the per-segment
// arithmetic of PropAMMVenue._merge and of the executor's fill sweep.
// Summing these floors matches onchain delivery to the wei.
func segmentOut(out, take, price *uint256.Int) {
	big256.MulWadDown(out, take, price)
}

// boardLive is PropAMMVenue._merge's inclusion test for one member board:
// the venue reported depth left (remaining != 0) and the board has not
// expired since it was read (the contract treats block.timestamp ==
// expiresAt as still live). The shape checks only guard against a malformed
// Extra; the venue never returns a live board without levels.
func boardLive(b *Board, now uint64) bool {
	if b.Remaining == nil || b.Remaining.IsZero() || b.Filled == nil {
		return false
	}
	n := len(b.Sizes)
	if n == 0 || len(b.Prices) != n {
		return false
	}
	return now <= b.ExpiresAt
}
