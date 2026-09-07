package biconomyprop

import "github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"

type Config struct {
	DexID   string              `json:"dexID"`
	ChainID valueobject.ChainID `json:"chainId"`
	// Venue is the PropAMMVenue: the single onchain entrypoint that merges
	// every registered maker board and splits fills across them internally.
	Venue string `json:"venue"`
}
