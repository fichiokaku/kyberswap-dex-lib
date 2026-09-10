package biconomyprop

import (
	"math/big"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
)

const (
	testVenue  = "0xb67f5cd266459f497d55694758f526cf26f12aea"
	testToken0 = "0x4200000000000000000000000000000000000006"
	testToken1 = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
)

func u(dec string) *uint256.Int {
	v, err := uint256.FromDecimal(dec)
	if err != nil {
		panic(err)
	}
	return v
}

func farExpiry() uint64 { return uint64(time.Now().Add(time.Hour).Unix()) }

func buildPool(t *testing.T, members []MemberExtra) *PoolSimulator {
	t.Helper()
	extra, err := json.Marshal(Extra{Members: members})
	require.NoError(t, err)
	staticExtra, err := json.Marshal(StaticExtra{Venue: testVenue})
	require.NoError(t, err)

	sim, err := NewPoolSimulator(entity.Pool{
		Address:     testVenue + "_" + testToken0 + "_" + testToken1,
		Exchange:    "biconomy-prop",
		Type:        DexType,
		Reserves:    entity.PoolReserves{"0", "0"},
		Tokens:      []*entity.PoolToken{{Address: testToken0}, {Address: testToken1}},
		Extra:       string(extra),
		StaticExtra: string(staticExtra),
		BlockNumber: 12345,
	})
	require.NoError(t, err)
	return sim
}

// twoMakers: member A has a two-rung board (1e18 @ 2000e18, then 2e18 more @
// 1999e18), member B a single rung (2e18 @ 2000e18, tying A's best price).
func twoMakers() []MemberExtra {
	exp := farExpiry()
	return []MemberExtra{
		{
			Maker: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Dir0: Board{
				Sizes:     []*uint256.Int{u("1000000000000000000"), u("3000000000000000000")},
				Prices:    []*uint256.Int{u("2000000000000000000000"), u("1999000000000000000000")},
				Filled:    u("0"),
				ExpiresAt: exp,
				Synced:    true,
			},
			Dir1: Board{Synced: false},
		},
		{
			Maker: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Dir0: Board{
				Sizes:     []*uint256.Int{u("2000000000000000000")},
				Prices:    []*uint256.Int{u("2000000000000000000000")},
				Filled:    u("0"),
				ExpiresAt: exp,
				Synced:    true,
			},
			Dir1: Board{Synced: false},
		},
	}
}

func calc(t *testing.T, sim *PoolSimulator, amountIn string) (*pool.CalcAmountOutResult, error) {
	t.Helper()
	in, ok := new(big.Int).SetString(amountIn, 10)
	require.True(t, ok)
	return sim.CalcAmountOut(pool.CalcAmountOutParams{
		TokenAmountIn: pool.TokenAmount{Token: testToken0, Amount: in},
		TokenOut:      testToken1,
	})
}

// The tie at 2000e18 must fill member A first (registry order): the venue's
// insertion sort swaps only on strictly-worse prices, so equal prices keep
// member order. 2e18 in = 1e18 from A's rung 1 + 1e18 from B's rung.
func TestCalcAmountOut_TieBreaksByRegistryOrder(t *testing.T) {
	sim := buildPool(t, twoMakers())
	res, err := calc(t, sim, "2000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, "4000000000000000000000", res.TokenAmountOut.Amount.String())

	swapInfo, ok := res.SwapInfo.(SwapInfo)
	require.True(t, ok)
	require.Len(t, swapInfo.Takes, 2)
	assert.Equal(t, 0, swapInfo.Takes[0].Member)
	assert.Equal(t, "1000000000000000000", swapInfo.Takes[0].AmountIn.Dec())
	assert.Equal(t, 1, swapInfo.Takes[1].Member)
	assert.Equal(t, "1000000000000000000", swapInfo.Takes[1].AmountIn.Dec())
}

// Full-depth walk across both makers and both price levels:
// 1e18@2000 (A) + 2e18@2000 (B) + 1e18@1999 (A) = 7999e18 out for 4e18 in.
func TestCalcAmountOut_SplitsAcrossMakersBestPriceFirst(t *testing.T) {
	sim := buildPool(t, twoMakers())
	res, err := calc(t, sim, "4000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, "7999000000000000000000", res.TokenAmountOut.Amount.String())

	swapInfo := res.SwapInfo.(SwapInfo)
	require.Len(t, swapInfo.Takes, 2)
	assert.Equal(t, "2000000000000000000", swapInfo.Takes[0].AmountIn.Dec()) // member A
	assert.Equal(t, "2000000000000000000", swapInfo.Takes[1].AmountIn.Dec()) // member B
}

// Per-segment floor division, checked at wei scale: 3 wei at price
// 1.333...e18 is floor(3 * 1333333333333333333 / 1e18) = 3, not 4.
func TestCalcAmountOut_FloorsPerSegment(t *testing.T) {
	exp := farExpiry()
	sim := buildPool(t, []MemberExtra{{
		Maker: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Dir0: Board{
			Sizes:     []*uint256.Int{u("1000000")},
			Prices:    []*uint256.Int{u("1333333333333333333")},
			Filled:    u("0"),
			ExpiresAt: exp,
			Synced:    true,
		},
		Dir1: Board{Synced: false},
	}})
	res, err := calc(t, sim, "3")
	require.NoError(t, err)
	assert.Equal(t, "3", res.TokenAmountOut.Amount.String())
}

// The venue reverts Inactive() when the merged book cannot cover amountIn;
// the simulator must reject rather than partially fill.
func TestCalcAmountOut_RejectsBeyondDepth(t *testing.T) {
	sim := buildPool(t, twoMakers())
	_, err := calc(t, sim, "5000000000000000001") // total live depth is 5e18
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}

// An expired board contributes nothing, exactly like _segments' expiry skip.
func TestCalcAmountOut_SkipsExpiredBoards(t *testing.T) {
	members := twoMakers()
	members[0].Dir0.ExpiresAt = uint64(time.Now().Add(-time.Minute).Unix())
	sim := buildPool(t, members)

	res, err := calc(t, sim, "2000000000000000000") // only B's 2e18 remains
	require.NoError(t, err)
	assert.Equal(t, "4000000000000000000000", res.TokenAmountOut.Amount.String())
	swapInfo := res.SwapInfo.(SwapInfo)
	require.Len(t, swapInfo.Takes, 1)
	assert.Equal(t, 1, swapInfo.Takes[0].Member)

	_, err = calc(t, sim, "2000000000000000001")
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}

// The lifetime cursor (filledByVersion) shifts the board's marginal rungs.
func TestCalcAmountOut_RespectsFilledCursor(t *testing.T) {
	members := twoMakers()
	members[0].Dir0.Filled = u("1000000000000000000") // A's 2000 rung already spent
	sim := buildPool(t, members)

	res, err := calc(t, sim, "3000000000000000000")
	require.NoError(t, err)
	// B 2e18@2000 (4000e18) + A 1e18@1999 (1999e18)
	assert.Equal(t, "5999000000000000000000", res.TokenAmountOut.Amount.String())
}

// UpdateBalance must advance cursors so repeated quoting walks the book the
// way sequential onchain fills would.
func TestUpdateBalance_AdvancesCursors(t *testing.T) {
	sim := buildPool(t, twoMakers())
	res, err := calc(t, sim, "2000000000000000000")
	require.NoError(t, err)
	sim.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: res.SwapInfo})

	// A cursor now 1e18 (its 2000 rung gone), B cursor 1e18 (1e18 left @2000).
	res2, err := calc(t, sim, "2000000000000000000")
	require.NoError(t, err)
	// B 1e18@2000 (2000e18) + A 1e18@1999 (1999e18)
	assert.Equal(t, "3999000000000000000000", res2.TokenAmountOut.Amount.String())
}

// Clones must not share cursor state with the original.
func TestCloneState_IsIndependent(t *testing.T) {
	sim := buildPool(t, twoMakers())
	clone := sim.CloneState().(*PoolSimulator)

	res, err := calc(t, sim, "2000000000000000000")
	require.NoError(t, err)
	sim.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: res.SwapInfo})

	resClone, err := calc(t, clone, "2000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, "4000000000000000000000", resClone.TokenAmountOut.Amount.String())

	resOrig, err := calc(t, sim, "2000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, "3999000000000000000000", resOrig.TokenAmountOut.Amount.String())
}

// Quoting and state updates must be deterministic across repetition.
func TestDeterminism(t *testing.T) {
	simA := buildPool(t, twoMakers())
	simB := buildPool(t, twoMakers())
	for i := 0; i < 3; i++ {
		resA, errA := calc(t, simA, "1500000000000000000")
		resB, errB := calc(t, simB, "1500000000000000000")
		require.NoError(t, errA)
		require.NoError(t, errB)
		assert.Equal(t, resA.TokenAmountOut.Amount.String(), resB.TokenAmountOut.Amount.String())
		simA.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: resA.SwapInfo})
		simB.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: resB.SwapInfo})
	}
}

func TestGetMetaInfo(t *testing.T) {
	sim := buildPool(t, twoMakers())
	meta, ok := sim.GetMetaInfo(testToken0, testToken1).(MetaInfo)
	require.True(t, ok)
	assert.Equal(t, testVenue, meta.Venue)
	assert.Equal(t, uint64(12345), meta.BlockNumber)
}
