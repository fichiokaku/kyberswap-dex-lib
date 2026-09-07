package biconomyprop

import (
	"errors"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

const (
	DexType = valueobject.ExchangeBiconomyProp

	methodGetPairs    = "getPairs"
	methodMemberPools = "memberPools"
	methodBoard       = "board"

	// defaultGas covers the venue's plan walk plus one member fill
	// (measured member pool.swap: ~114k); each extra member fill adds
	// roughly one more stored-door execution.
	defaultGas   = 220_000
	perMemberGas = 130_000
)

var (
	ErrInvalidToken          = errors.New("biconomy-prop: tokenIn/tokenOut is not the pool pair")
	ErrZeroAmountIn          = errors.New("biconomy-prop: zero amountIn")
	ErrInsufficientLiquidity = errors.New("biconomy-prop: amountIn exceeds live board depth (venue swap would revert Inactive)")
	ErrZeroAmountOut         = errors.New("biconomy-prop: zero amountOut")
	ErrOverflow              = errors.New("biconomy-prop: uint256 overflow")
	ErrInvalidSwapInfo       = errors.New("biconomy-prop: invalid swapInfo")
)
