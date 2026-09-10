package biconomyprop

import "github.com/holiman/uint256"

// StaticExtra is immutable per-pool metadata set at discovery time.
type StaticExtra struct {
	// Venue is the PropAMMVenue contract this pool entity fronts. The
	// entity address is synthetic (venue_token0_token1) because one venue
	// serves many pairs; execution always targets Venue.
	Venue string `json:"venue"`
}

// Board is one maker's live ladder for ONE direction, exactly as
// PropAMMVenue.board(mm, tokenIn, tokenOut) returns it: pack-rounded
// cumulative sizes and prices (the executor stores 48-bit mantissa floors;
// the venue mirrors them), the version's lifetime fill cursor, and the
// board expiry. Quotes off these
// values equal stored-door deliveries to the wei.
type Board struct {
	Sizes     []*uint256.Int `json:"sizes"`  // cumulative, ascending
	Prices    []*uint256.Int `json:"prices"` // 1e18-scaled tokenOut-wei per tokenIn-wei
	Filled    *uint256.Int   `json:"filled"` // lifetime cursor for the active version
	ExpiresAt uint64         `json:"expiresAt"`
	Synced    bool           `json:"synced"`
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
// price sort is stable, so equal-priced segments fill in registry order.
type Extra struct {
	Members []MemberExtra `json:"members"`
}

// memberTake is one member's input allocation for a simulated swap.
type memberTake struct {
	Member   int          `json:"member"`
	AmountIn *uint256.Int `json:"amountIn"`
}

// SwapInfo carries the per-member allocation CalcAmountOut planned, so
// UpdateBalance can advance the same cursors the onchain fill would.
type SwapInfo struct {
	DirIndex  int          `json:"dirIndex"` // 0: token0->token1, 1: token1->token0
	Takes     []memberTake `json:"takes"`
	AmountOut *uint256.Int `json:"amountOut"`
}

type MetaInfo struct {
	Venue       string `json:"venue"`
	BlockNumber uint64 `json:"blockNumber"`
}
