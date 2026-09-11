package biconomyprop

import "github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"

type Config struct {
	DexID   string              `json:"dexID"`
	ChainID valueobject.ChainID `json:"chainId"`
	// Venue is the PropAMMVenue: the single onchain entrypoint that merges
	// every registered maker board and splits fills across them internally.
	//
	// TODO: the venue and the executor behind it are being redeployed with
	// the new board() read surface (no synced flag). The address configured
	// here must be updated to the new PropAMMVenue deployment; the previous
	// deployment does not match this package's ABI.
	Venue string `json:"venue"`
}
