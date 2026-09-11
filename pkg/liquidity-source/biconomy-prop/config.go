package biconomyprop

import "github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"

type Config struct {
	DexID   string              `json:"dexID"`
	ChainID valueobject.ChainID `json:"chainId"`
	// Venue is the PropAMMVenue: the single onchain entrypoint that merges
	// every registered maker board and splits fills across them internally.
	// The canonical deployment is 0x000000445Dff11123a3BD8A4Dd03a351829aF892
	// on every supported chain.
	Venue string `json:"venue"`
}
