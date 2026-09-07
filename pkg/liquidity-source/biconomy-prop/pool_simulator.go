package biconomyprop

import (
	"math/big"
	"sort"
	"time"

	"github.com/goccy/go-json"
	"github.com/holiman/uint256"
	"github.com/samber/lo"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	bignum "github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/bignumber"
)

// PoolSimulator replays PropAMMVenue's plan math exactly: live marginal
// segments across every member board, merged price-descending (stable, so
// equal prices fill in registry order like the venue's insertion sort), then
// consumed best-first with floor division per segment. The venue's own quote
// equals the sum of member stored-door deliveries, so this simulator's
// output equals onchain delivery to the wei.
type PoolSimulator struct {
	pool.Pool

	staticExtra StaticExtra
	members     []MemberExtra

	// cursors[i][d] is member i's lifetime fill cursor for direction d,
	// advanced by UpdateBalance. Boards themselves are immutable after
	// construction, so clones share them and deep-copy only the cursors.
	cursors [][2]*uint256.Int

	// now is the construction-time clock used for board expiry, keeping
	// CalcAmountOut pure and deterministic per instance.
	now uint64
}

var _ = pool.RegisterFactory0(DexType, NewPoolSimulator)

func NewPoolSimulator(ep entity.Pool) (*PoolSimulator, error) {
	if len(ep.Tokens) != 2 || len(ep.Reserves) != 2 {
		return nil, ErrInvalidToken
	}

	var extra Extra
	if err := json.Unmarshal([]byte(ep.Extra), &extra); err != nil {
		return nil, err
	}
	var staticExtra StaticExtra
	if err := json.Unmarshal([]byte(ep.StaticExtra), &staticExtra); err != nil {
		return nil, err
	}

	cursors := make([][2]*uint256.Int, len(extra.Members))
	for i := range extra.Members {
		cursors[i][0] = cursorOf(&extra.Members[i].Dir0)
		cursors[i][1] = cursorOf(&extra.Members[i].Dir1)
	}

	return &PoolSimulator{
		Pool: pool.Pool{Info: pool.PoolInfo{
			Address:     ep.Address,
			Exchange:    ep.Exchange,
			Type:        ep.Type,
			Tokens:      lo.Map(ep.Tokens, func(t *entity.PoolToken, _ int) string { return t.Address }),
			Reserves:    lo.Map(ep.Reserves, func(s string, _ int) *big.Int { return bignum.NewBig(s) }),
			BlockNumber: ep.BlockNumber,
		}},
		staticExtra: staticExtra,
		members:     extra.Members,
		cursors:     cursors,
		now:         uint64(time.Now().Unix()),
	}, nil
}

func cursorOf(b *Board) *uint256.Int {
	if b.Filled == nil {
		return new(uint256.Int)
	}
	return b.Filled.Clone()
}

// segment is one member's marginal rung in the merged book.
type segment struct {
	member int
	size   *uint256.Int
	price  *uint256.Int
}

// segments replicates PropAMMVenue._segments for one direction against the
// CURRENT cursors: every live board's marginal rungs, merged and sorted
// price-descending. sort.SliceStable matches the contract's insertion sort
// (strictly-less swaps): equal prices keep member registry order.
func (s *PoolSimulator) segments(dirIndex int) []segment {
	var segs []segment
	for i := range s.members {
		board := &s.members[i].Dir0
		if dirIndex == 1 {
			board = &s.members[i].Dir1
		}
		if !boardLive(board, s.now) {
			continue
		}
		cursor := s.cursors[i][dirIndex]
		if cursor.Cmp(board.Sizes[len(board.Sizes)-1]) >= 0 {
			continue
		}
		prev := cursor
		for j := range board.Sizes {
			if prev.Cmp(board.Sizes[j]) >= 0 {
				continue
			}
			segs = append(segs, segment{
				member: i,
				size:   new(uint256.Int).Sub(board.Sizes[j], prev),
				price:  board.Prices[j],
			})
			prev = board.Sizes[j]
		}
	}
	sort.SliceStable(segs, func(a, b int) bool { return segs[a].price.Cmp(segs[b].price) > 0 })
	return segs
}

func (s *PoolSimulator) CalcAmountOut(params pool.CalcAmountOutParams) (*pool.CalcAmountOutResult, error) {
	indexIn, indexOut := s.GetTokenIndex(params.TokenAmountIn.Token), s.GetTokenIndex(params.TokenOut)
	if indexIn < 0 || indexOut < 0 || indexIn == indexOut {
		return nil, ErrInvalidToken
	}
	amountIn, overflow := uint256.FromBig(params.TokenAmountIn.Amount)
	if overflow {
		return nil, ErrOverflow
	}
	if amountIn.IsZero() {
		return nil, ErrZeroAmountIn
	}

	dirIndex := 0
	if indexIn == 1 {
		dirIndex = 1
	}

	var (
		out       uint256.Int
		segOut    uint256.Int
		take      uint256.Int
		remaining = amountIn.Clone()
		allocs    = map[int]*uint256.Int{}
	)
	for _, seg := range s.segments(dirIndex) {
		if remaining.IsZero() {
			break
		}
		take.Set(seg.size)
		if take.Cmp(remaining) > 0 {
			take.Set(remaining)
		}
		segmentOut(&segOut, &take, seg.price)
		out.Add(&out, &segOut)
		remaining.Sub(remaining, &take)
		if a, ok := allocs[seg.member]; ok {
			a.Add(a, &take)
		} else {
			allocs[seg.member] = take.Clone()
		}
	}
	// PropAMMVenue.swap reverts Inactive() unless the merged book covers the
	// full amountIn; there are no partial fills at the venue surface.
	if !remaining.IsZero() {
		return nil, ErrInsufficientLiquidity
	}
	if out.IsZero() {
		return nil, ErrZeroAmountOut
	}

	takes := make([]memberTake, 0, len(allocs))
	for member, amount := range allocs {
		takes = append(takes, memberTake{Member: member, AmountIn: amount})
	}
	sort.Slice(takes, func(a, b int) bool { return takes[a].Member < takes[b].Member })

	amountOut := out.Clone()
	return &pool.CalcAmountOutResult{
		TokenAmountOut: &pool.TokenAmount{Token: params.TokenOut, Amount: amountOut.ToBig()},
		Fee:            &pool.TokenAmount{Token: params.TokenOut, Amount: bignum.ZeroBI},
		Gas:            defaultGas + int64(len(takes)-1)*perMemberGas,
		SwapInfo:       SwapInfo{DirIndex: dirIndex, Takes: takes, AmountOut: amountOut},
	}, nil
}

// UpdateBalance advances each touched member's lifetime cursor by its
// allocation, exactly as the onchain fills advance filledByVersion, and
// reduces the delivered token's reserve.
func (s *PoolSimulator) UpdateBalance(params pool.UpdateBalanceParams) {
	swapInfo, ok := params.SwapInfo.(SwapInfo)
	if !ok {
		return
	}
	for _, take := range swapInfo.Takes {
		if take.Member < 0 || take.Member >= len(s.cursors) || take.AmountIn == nil {
			continue
		}
		cursor := s.cursors[take.Member][swapInfo.DirIndex]
		cursor.Add(cursor, take.AmountIn)
	}

	outIdx := 1 - swapInfo.DirIndex
	reserveOut := new(big.Int).Sub(s.Info.Reserves[outIdx], swapInfo.AmountOut.ToBig())
	if reserveOut.Sign() < 0 {
		reserveOut.SetInt64(0)
	}
	s.Info.Reserves[outIdx] = reserveOut
}

func (s *PoolSimulator) CloneState() pool.IPoolSimulator {
	cloned := *s
	cloned.cursors = make([][2]*uint256.Int, len(s.cursors))
	for i := range s.cursors {
		cloned.cursors[i][0] = s.cursors[i][0].Clone()
		cloned.cursors[i][1] = s.cursors[i][1].Clone()
	}
	cloned.Info.Reserves = lo.Map(s.Info.Reserves, func(r *big.Int, _ int) *big.Int { return new(big.Int).Set(r) })
	return &cloned
}

// GetMetaInfo names the venue: the entity address is synthetic (one venue,
// many pairs), so executors must target Venue with the push-payment
// swap(tokenIn, tokenOut, amountIn, minAmountOut, recipient, deadline),
// amountIn at calldata byte offset 68.
func (s *PoolSimulator) GetMetaInfo(_, _ string) any {
	return MetaInfo{Venue: s.staticExtra.Venue, BlockNumber: s.Info.BlockNumber}
}
