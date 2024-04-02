package rpc

import (
	"context"
)

type HeliusClient struct {
	*Client
}

func NewHelius(rpcEndpoint string) *HeliusClient {
	return &HeliusClient{
		Client: New(rpcEndpoint),
	}
}

type GetAssetOpts struct {
	Id             string                      `json:"id"`
	DisplayOptions *GetAssetOptsDisplayOptions `json:"displayOptions"`
}

type GetAssetOptsDisplayOptions struct {
	ShowUnverifiedCollections bool `json:"showUnverifiedCollections"`
	ShowCollectionMetadata    bool `json:"showCollectionMetadata"`
	ShowFungible              bool `json:"showFungible"`
	ShowInscription           bool `json:"showInscription"`
}

func (cl *HeliusClient) GetAsset(
	ctx context.Context,
	opts *GetAssetOpts,
) (out *GetAssetResult, err error) {
	params := M{}

	if opts != nil {
		if opts.Id != "" {
			params["id"] = opts.Id
		}
		if opts.DisplayOptions != nil {
			params["displayOptions"] = opts.DisplayOptions
		}
	}

	err = cl.rpcClient.CallForInto(ctx, &out, "getAsset", params)

	if err != nil {
		return nil, err
	}

	if out == nil {
		return nil, ErrNotFound
	}

	return out, nil
}

type GetAssetResult struct {
	Interface      string                  `json:"interface"` // enum :V1_NFT V1_PRINT LEGACY_NFT V2_NFT FungibleAsset FungibleToken Custom Identity Executable ProgrammableNFT
	Id             string                  `json:"id"`
	Content        *GetAssetContent        `json:"content"`
	Authorities    []GetAssetAuthorities   `json:"authorities"`
	Compression    *GetAssetCompression    `json:"compression"`
	Grouping       []GetAssetGrouping      `json:"grouping"`
	Royalty        *GetAssetRoyalty        `json:"royalty"`
	Creators       []GetAssetCreators      `json:"creators"`
	Ownership      *GetAssetOwnership      `json:"ownership"`
	MintExtensions *GetAssetMintExtensions `json:"mint_extensions"`
	Supply         *GetAssetSupply         `json:"supply"`
	TokenInfo      *GetAssetTokenInfo      `json:"token_info"`
	Inscription    *GetAssetInscription    `json:"inscription"`
	SPL20          *GetAssetSPL20          `json:"spl20"`
}

type GetAssetContent struct {
	Schema   string            `json:"$schema"`
	JsonURI  string            `json:"json_uri"`
	Files    []GetAssetFile    `json:"files"`
	Metadata *GetAssetMetadata `json:"metadata"`
	Links    *GetAssetLinks    `json:"links"`
}

type GetAssetFile struct {
	Uri    string `json:"uri"`
	CdnURI string `json:"cdn_uri"`
	Mime   string `json:"mime"`
}

type GetAssetMetadata struct {
	Description   string `json:"description"`
	Name          string `json:"name"`
	Symbol        string `json:"symbol"`
	TokenStandard string `json:"token_standard"`
	Attributes    []any  `json:"attributes"`
}

type GetAssetLinks struct {
	ExternalURL string `json:"external_url"`
}

type GetAssetAuthorities struct {
	Address string   `json:"address"`
	Scopes  []string `json:"scopes"`
}

type GetAssetCompression struct {
	Eligible    bool   `json:"eligible"`
	Compressed  bool   `json:"compressed"`
	DataHash    string `json:"data_hash"`
	CreatorHash string `json:"creator_hash"`
	AssetHash   string `json:"asset_hash"`
	Tree        string `json:"tree"`
	Seq         uint64 `json:"seq"`
	LeafID      uint64 `json:"leaf_id"`
}

type GetAssetGrouping struct {
	GroupKey   string `json:"group_key"`
	GroupValue string `json:"group_value"`
}

type GetAssetRoyalty struct {
	RoayltyModel        string  `json:"royalty_model"`
	Target              *string `json:"target"`
	Percent             float64 `json:"percent"`
	BasisPoints         uint64  `json:"basis_points"`
	PrimarySaleHappened bool    `json:"primary_sale_happened"`
	Locked              bool    `json:"locked"`
}

type GetAssetCreators struct {
	Address  string `json:"address"`
	Share    uint64 `json:"share"`
	Verified bool   `json:"verified"`
}

type GetAssetOwnership struct {
	Frozen         bool    `json:"frozen"`
	Delegated      bool    `json:"delegated"`
	Delegate       *string `json:"delegate"`
	OwnershipModel string  `json:"ownership_model"`
	Owner          string  `json:"owner"`
	Supply         *string `json:"supply"`
	Mutable        bool    `json:"mutable"`
	Burnt          bool    `json:"burnt"`
}

type GetAssetMintExtensions struct {
	// TODO
}

type GetAssetSupply struct {
	PrintMaxSupply     uint64  `json:"print_max_supply"`
	PrintCurrentSupply uint64  `json:"print_current_supply"`
	EditionNonce       uint64  `json:"edition_nonce"`
	EditionNumber      *uint64 `json:"edition_number"`
	MasterEditionMint  *string `json:"master_edition_mint"`
}

type GetAssetTokenInfo struct {
	Symbol       string             `json:"symbol"`
	Supply       uint64             `json:"supply"`
	Decimals     uint8              `json:"decimals"`
	TokenProgram string             `json:"token_program"`
	PriceInfo    *GetAssetPriceInfo `json:"price_info"`
}

type GetAssetPriceInfo struct {
	PricePerToken float64 `json:"price_per_token"`
	Currency      string  `json:"currency"`
}

type GetAssetInscription struct {
	// TODO
}

type GetAssetSPL20 struct {
	// TODO
}
