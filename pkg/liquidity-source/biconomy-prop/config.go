package biconomyprop

import "github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"

type Config struct {
	DexID   string              `json:"dexID"`
	ChainID valueobject.ChainID `json:"chainId"`
	// Venue is the PropAMMVenue: the single onchain entrypoint that merges
	// every registered maker board and splits fills across them internally.
	// The canonical deployment is 0x000000a22FAC0B743934423f1A6073147b246Edb
	// on every supported chain.
	Venue string `json:"venue"`
}
