package biconomyprop

import (
	"errors"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

const (
	DexType = valueobject.ExchangeBiconomyProp

	methodGetPairs = "getPairs"
	methodMakers   = "makers"
	methodBoard    = "board"

	// defaultGas covers the venue's plan walk plus one maker fill through the
	// executor's stored door; each extra maker adds roughly one more fill.
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
