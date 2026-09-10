package biconomyprop

import (
	"context"
	"math/big"
	"time"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/goccy/go-json"
	"github.com/holiman/uint256"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	pooltrack "github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool/tracker"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/big256"
)

type PoolTracker struct {
	config       *Config
	ethrpcClient *ethrpc.Client
}

var _ = pooltrack.RegisterFactoryCE(DexType, NewPoolTracker)

func NewPoolTracker(cfg *Config, ethrpcClient *ethrpc.Client) (*PoolTracker, error) {
	return &PoolTracker{config: cfg, ethrpcClient: ethrpcClient}, nil
}

// rawBoard mirrors PropAMMVenue.board()'s return tuple.
type rawBoard struct {
	Sizes     []*big.Int
	Prices    []*big.Int
	Filled    *big.Int
	Remaining *big.Int
	ExpiresAt *big.Int
	Synced    bool
}

// GetNewPoolState refreshes the pair's full multi-maker state: the venue's
// maker registry, then venue.board(maker, ...) for BOTH directions of this
// pair, pinned to the registry call's resolved block so the merged view is
// internally consistent. board() returns the pack-rounded rungs the
// executor's stored door prices at plus the version's lifetime fill cursor,
// so the simulator replays onchain deliveries exactly.
func (t *PoolTracker) GetNewPoolState(
	ctx context.Context,
	p entity.Pool,
	_ pool.GetNewPoolStateParams,
) (entity.Pool, error) {
	if len(p.Tokens) != 2 {
		return p, ErrInvalidToken
	}
	token0, token1 := common.HexToAddress(p.Tokens[0].Address), common.HexToAddress(p.Tokens[1].Address)

	var staticExtra StaticExtra
	if err := json.Unmarshal([]byte(p.StaticExtra), &staticExtra); err != nil {
		return p, err
	}

	var makerAddrs []common.Address
	makersResp, err := t.ethrpcClient.NewRequest().SetContext(ctx).
		AddCall(&ethrpc.Call{ABI: venueABI, Target: staticExtra.Venue, Method: methodMakers}, []any{&makerAddrs}).
		Aggregate()
	if err != nil {
		return p, err
	}

	boards := make([]rawBoard, 2*len(makerAddrs))
	if len(makerAddrs) > 0 {
		req := t.ethrpcClient.NewRequest().SetContext(ctx).SetBlockNumber(makersResp.BlockNumber)
		for i, m := range makerAddrs {
			req.AddCall(&ethrpc.Call{ABI: venueABI, Target: staticExtra.Venue, Method: methodBoard, Params: []any{m, token0, token1}},
				[]any{&boards[2*i]})
			req.AddCall(&ethrpc.Call{ABI: venueABI, Target: staticExtra.Venue, Method: methodBoard, Params: []any{m, token1, token0}},
				[]any{&boards[2*i+1]})
		}
		if _, err := req.Aggregate(); err != nil {
			return p, err
		}
	}

	members := make([]MemberExtra, len(makerAddrs))
	for i, m := range makerAddrs {
		members[i] = MemberExtra{
			Maker: hexutil.Encode(m[:]),
			Dir0:  toBoard(boards[2*i]),
			Dir1:  toBoard(boards[2*i+1]),
		}
	}

	extra, err := json.Marshal(Extra{Members: members})
	if err != nil {
		return p, err
	}

	// Reserves report each token's DELIVERABLE amount: the floor-math output
	// of sweeping every live board of the direction that pays that token out.
	now := uint64(time.Now().Unix())
	reserve1 := deliverable(members, 0, now) // token0 -> token1 pays out token1
	reserve0 := deliverable(members, 1, now) // token1 -> token0 pays out token0

	p.Extra = string(extra)
	p.Reserves = entity.PoolReserves{reserve0.Dec(), reserve1.Dec()}
	p.BlockNumber = makersResp.BlockNumber.Uint64()
	p.Timestamp = time.Now().Unix()
	return p, nil
}

func toBoard(rb rawBoard) Board {
	b := Board{
		Sizes:     make([]*uint256.Int, len(rb.Sizes)),
		Prices:    make([]*uint256.Int, len(rb.Prices)),
		Filled:    big256.FromBig(rb.Filled),
		Synced:    rb.Synced,
		ExpiresAt: 0,
	}
	if rb.ExpiresAt != nil {
		b.ExpiresAt = rb.ExpiresAt.Uint64()
	}
	for i, s := range rb.Sizes {
		b.Sizes[i] = big256.FromBig(s)
	}
	for i, pr := range rb.Prices {
		b.Prices[i] = big256.FromBig(pr)
	}
	return b
}

// deliverable sums, over every live board of a direction, the exact
// per-segment floor output of consuming that board's full remaining depth.
func deliverable(members []MemberExtra, dirIndex int, now uint64) *uint256.Int {
	total := new(uint256.Int)
	for i := range members {
		board := &members[i].Dir0
		if dirIndex == 1 {
			board = &members[i].Dir1
		}
		if !boardLive(board, now) {
			continue
		}
		cursor := board.Filled.Clone()
		var out, take, seg uint256.Int
		for j := range board.Sizes {
			if cursor.Cmp(board.Sizes[j]) >= 0 {
				continue
			}
			seg.Sub(board.Sizes[j], cursor)
			take.Set(&seg)
			segmentOut(&out, &take, board.Prices[j])
			total.Add(total, &out)
			cursor.Set(board.Sizes[j])
		}
	}
	return total
}
