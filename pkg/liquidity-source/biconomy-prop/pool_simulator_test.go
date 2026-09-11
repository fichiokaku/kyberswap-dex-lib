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
	// The canonical PropAMMVenue address; a label in these tests, nothing is fetched.
	testVenue  = "0x000000a22FAC0B743934423f1A6073147b246Edb"
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

// liveBoard builds a Board the way the venue reports a live one: exact
// levels, the consumed meter and remaining = top size - filled.
func liveBoard(sizes, prices []string, filled string, expiresAt uint64) Board {
	if len(sizes) == 0 || len(sizes) != len(prices) {
		panic("liveBoard: sizes and prices must be non-empty and equal length")
	}
	b := Board{
		Sizes:     make([]*uint256.Int, len(sizes)),
		Prices:    make([]*uint256.Int, len(prices)),
		Filled:    u(filled),
		ExpiresAt: expiresAt,
	}
	for i := range sizes {
		b.Sizes[i] = u(sizes[i])
		b.Prices[i] = u(prices[i])
	}
	top := b.Sizes[len(b.Sizes)-1]
	b.Remaining = new(uint256.Int)
	if b.Filled.Cmp(top) < 0 {
		b.Remaining.Sub(top, b.Filled)
	}
	return b
}

// darkBoard is what the venue returns for a maker with no committed board
// in a direction: no levels, zero meter, zero remaining, zero expiry.
func darkBoard() Board {
	return Board{Filled: u("0"), Remaining: u("0")}
}

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
// Neither quotes the reverse direction.
func twoMakers() []MemberExtra {
	exp := farExpiry()
	return []MemberExtra{
		{
			Maker: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Dir0: liveBoard(
				[]string{"1000000000000000000", "3000000000000000000"},
				[]string{"2000000000000000000000", "1999000000000000000000"},
				"0", exp,
			),
			Dir1: darkBoard(),
		},
		{
			Maker: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Dir0: liveBoard(
				[]string{"2000000000000000000"},
				[]string{"2000000000000000000000"},
				"0", exp,
			),
			Dir1: darkBoard(),
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

func calcReverse(t *testing.T, sim *PoolSimulator, amountIn string) (*pool.CalcAmountOutResult, error) {
	t.Helper()
	in, ok := new(big.Int).SetString(amountIn, 10)
	require.True(t, ok)
	return sim.CalcAmountOut(pool.CalcAmountOutParams{
		TokenAmountIn: pool.TokenAmount{Token: testToken1, Amount: in},
		TokenOut:      testToken0,
	})
}

// The tie at 2000e18 must fill member A first (registry order): the venue's
// merge takes another maker's rung only on a strictly better price, so equal
// prices keep member order. 2e18 in = 1e18 from A's rung 1 + 1e18 from B's rung.
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
	sim := buildPool(t, []MemberExtra{{
		Maker: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Dir0:  liveBoard([]string{"1000000"}, []string{"1333333333333333333"}, "0", farExpiry()),
		Dir1:  darkBoard(),
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

// PropAMMVenue._merge drops a maker allocation whose floored output is zero
// and stops counting it as coverage, so quote/swap revert Inactive() for that
// size. Maker B quotes 0.5e18: one wei spilling onto it after A's top size
// delivers floor(1 * 0.5) = 0 and the whole swap is rejected; two wei deliver
// floor(2 * 0.5) = 1 unit on top of A's output.
func TestCalcAmountOut_ZeroOutputAllocationIsNotCoverage(t *testing.T) {
	exp := farExpiry()
	sim := buildPool(t, []MemberExtra{
		{
			Maker: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Dir0:  liveBoard([]string{"1000000000000000000"}, []string{"2000000000000000000000"}, "0", exp),
			Dir1:  darkBoard(),
		},
		{
			Maker: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Dir0:  liveBoard([]string{"1000000000000000000"}, []string{"500000000000000000"}, "0", exp),
			Dir1:  darkBoard(),
		},
	})

	_, err := calc(t, sim, "1000000000000000001") // A's top size + 1 wei
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)

	res, err := calc(t, sim, "1000000000000000002") // A's top size + 2 wei
	require.NoError(t, err)
	assert.Equal(t, "2000000000000000000001", res.TokenAmountOut.Amount.String())
	swapInfo := res.SwapInfo.(SwapInfo)
	require.Len(t, swapInfo.Takes, 2)
	assert.Equal(t, 0, swapInfo.Takes[0].Member)
	assert.Equal(t, "1000000000000000000", swapInfo.Takes[0].AmountIn.Dec())
	assert.Equal(t, 1, swapInfo.Takes[1].Member)
	assert.Equal(t, "2", swapInfo.Takes[1].AmountIn.Dec())

	// Exactly A's top size never touches B and is unaffected by the rule.
	res, err = calc(t, sim, "1000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, "2000000000000000000000", res.TokenAmountOut.Amount.String())
}

// An expired board contributes nothing, exactly like _merge's remaining == 0
// skip once the executor stops reporting it live.
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

// A board the venue reported with remaining == 0 is out of the merge even if
// it still carries levels (exhausted); a dark board has no levels at all. In
// the reverse direction both makers are dark, so there is no liquidity.
func TestCalcAmountOut_SkipsDarkAndExhaustedBoards(t *testing.T) {
	members := twoMakers()
	// Exhaust A: meter at the top size, so the venue reports remaining 0.
	members[0].Dir0 = liveBoard(
		[]string{"1000000000000000000", "3000000000000000000"},
		[]string{"2000000000000000000000", "1999000000000000000000"},
		"3000000000000000000", farExpiry(),
	)
	require.True(t, members[0].Dir0.Remaining.IsZero())
	sim := buildPool(t, members)

	res, err := calc(t, sim, "2000000000000000000") // only B's 2e18
	require.NoError(t, err)
	assert.Equal(t, "4000000000000000000000", res.TokenAmountOut.Amount.String())
	swapInfo := res.SwapInfo.(SwapInfo)
	require.Len(t, swapInfo.Takes, 1)
	assert.Equal(t, 1, swapInfo.Takes[0].Member)

	_, err = calcReverse(t, sim, "1")
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
}

// The consumed meter (filled) shifts the board's marginal rungs.
func TestCalcAmountOut_RespectsFilledCursor(t *testing.T) {
	members := twoMakers()
	members[0].Dir0 = liveBoard( // A's 2000 rung already spent
		[]string{"1000000000000000000", "3000000000000000000"},
		[]string{"2000000000000000000000", "1999000000000000000000"},
		"1000000000000000000", farExpiry(),
	)
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

// Once simulated fills exhaust a board the venue reported live, it drops out
// of the merge without touching the (immutable) Board.
func TestUpdateBalance_ExhaustsBoard(t *testing.T) {
	sim := buildPool(t, twoMakers())
	res, err := calc(t, sim, "5000000000000000000") // the whole book
	require.NoError(t, err)
	sim.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: res.SwapInfo})

	_, err = calc(t, sim, "1")
	assert.ErrorIs(t, err, ErrInsufficientLiquidity)
	assert.Equal(t, "0", sim.Info.Reserves[1].String())
	assert.True(t, sim.members[0].Dir0.Filled.IsZero(), "Board.Filled must stay as tracked")
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
