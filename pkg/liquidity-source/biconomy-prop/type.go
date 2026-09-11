package biconomyprop

import "github.com/holiman/uint256"

// StaticExtra is immutable per-pool metadata set at discovery time.
type StaticExtra struct {
	// Venue is the PropAMMVenue contract this pool entity fronts. The
	// entity address is synthetic (venue_token0_token1) because one venue
	// serves many pairs; execution always targets Venue.
	Venue string `json:"venue"`
}

// Board is one maker's board for ONE direction, exactly as
// PropAMMVenue.board(mm, tokenIn, tokenOut) returns it. Boards live in the
// executor and the venue reads them straight through, so Sizes and Prices
// are the exact levels the executor's fill sweeps at right now (boards
// priced relative to an anchor come back already resolved). Filled is the
// tokenIn consumed in the current depth version, Remaining the depth left
// past it and ExpiresAt the effective expiry. A dark, expired or exhausted
// board reports Remaining == 0; a dark board also has no levels.
type Board struct {
	Sizes     []*uint256.Int `json:"sizes"`     // cumulative, ascending
	Prices    []*uint256.Int `json:"prices"`    // 1e18-scaled tokenOut-wei per tokenIn-wei
	Filled    *uint256.Int   `json:"filled"`    // tokenIn consumed in the current depth version
	Remaining *uint256.Int   `json:"remaining"` // Sizes[last] - Filled while live, else 0
	ExpiresAt uint64         `json:"expiresAt"` // unix seconds; 0 for a dark board
}

// MemberExtra is one registered maker's state for both directions of this
// pair. Dir0 quotes token0 -> token1; Dir1 quotes token1 -> token0.
type MemberExtra struct {
	Maker string `json:"maker"` // the maker's signer address, as registered on the venue
	Dir0  Board  `json:"dir0"`
	Dir1  Board  `json:"dir1"`
}

// Extra is rewritten end-to-end on each tracker refresh. Member order is the
// venue's registry order, which the simulator must preserve: the venue's
// merge takes a maker's rung only on a strictly better price, so
// equal-priced segments fill in registry order.
type Extra struct {
	Members []MemberExtra `json:"members"`
}

// memberTake is one member's input allocation for a simulated swap.
type memberTake struct {
	Member   int          `json:"member"`
	AmountIn *uint256.Int `json:"amountIn"`
}

// SwapInfo carries the per-member allocation CalcAmountOut planned, so
// UpdateBalance can advance the same meters the onchain fill would.
type SwapInfo struct {
	DirIndex  int          `json:"dirIndex"` // 0: token0->token1, 1: token1->token0
	Takes     []memberTake `json:"takes"`
	AmountOut *uint256.Int `json:"amountOut"`
}

type MetaInfo struct {
	Venue       string `json:"venue"`
	BlockNumber uint64 `json:"blockNumber"`
}
