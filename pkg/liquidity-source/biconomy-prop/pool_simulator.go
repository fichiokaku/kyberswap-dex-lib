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

// PoolSimulator replays PropAMMVenue's merge exactly: live marginal
// segments across every member board, ordered price-descending (stable, so
// equal prices fill in registry order like the venue's k-way merge), then
// consumed best-first with floor division per segment, and a maker whose
// allocation floors to zero output counted as uncovered. The venue's quote is
// the sum of the per-maker fills the executor performs, so this simulator's
// output equals onchain delivery to the wei.
type PoolSimulator struct {
	pool.Pool

	staticExtra StaticExtra
	members     []MemberExtra

	// cursors[i][d] is member i's consumed meter for direction d, seeded
	// from Board.Filled and advanced by UpdateBalance. Boards themselves are
	// immutable after construction, so clones share them and deep-copy only
	// the cursors.
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

// allocation is one member's planned input and floored output for a swap.
type allocation struct {
	in  *uint256.Int
	out *uint256.Int
}

// segments lists, for one direction and against the CURRENT cursors, every
// live board's marginal rungs sorted price-descending. This is the order
// PropAMMVenue._merge consumes them in: prices never improve with depth
// inside a board, and the merge takes a maker's rung only on a strictly
// better price, so sort.SliceStable's tie handling (member registry order)
// matches the contract. A board the venue reported live can still be
// exhausted by earlier simulated fills, hence the cursor check.
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
		allocs    = map[int]*allocation{}
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
		a, ok := allocs[seg.member]
		if !ok {
			a = &allocation{in: new(uint256.Int), out: new(uint256.Int)}
			allocs[seg.member] = a
		}
		a.in.Add(a.in, &take)
		a.out.Add(a.out, &segOut)
	}
	// PropAMMVenue._merge counts a maker's allocation as coverage only when
	// its floored output is non-zero: a few wei landing on a maker priced
	// below 1e18 would deliver nothing, so the venue drops that allocation and
	// quote/swap revert Inactive() for that exact size. Either way the venue
	// never partially fills, so the simulator rejects rather than trims.
	if !remaining.IsZero() {
		return nil, ErrInsufficientLiquidity
	}
	for _, a := range allocs {
		if a.out.IsZero() {
			return nil, ErrInsufficientLiquidity
		}
	}
	if out.IsZero() {
		return nil, ErrZeroAmountOut
	}

	takes := make([]memberTake, 0, len(allocs))
	for member, a := range allocs {
		takes = append(takes, memberTake{Member: member, AmountIn: a.in})
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

// UpdateBalance advances each touched member's cursor by its allocation,
// exactly as the executor's fill advances the board's filled meter, and
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
