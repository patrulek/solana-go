package ws

import (
	"context"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type TransactionResult struct {
	Transaction struct {
		Transaction []string `json:"transaction"`
		Meta        struct {
			Err    interface{} `json:"err"`
			Status struct {
				Ok interface{} `json:"Ok"`
			} `json:"status"`
			Fee               uint64        `json:"fee"`
			PreBalances       []uint64      `json:"preBalances"`
			PostBalances      []uint64      `json:"postBalances"`
			InnerInstructions []interface{} `json:"innerInstructions"`
			LogMessages       []string      `json:"logMessages"`
			PreTokenBalances  []interface{} `json:"preTokenBalances"`
			PostTokenBalances []interface{} `json:"postTokenBalances"`
			Rewards           interface{}   `json:"rewards"`
			LoadedAddresses   struct {
				Writable []string `json:"writable"`
				Readable []string `json:"readable"`
			} `json:"loadedAddresses"`
			ComputeUnitsConsumed uint64 `json:"computeUnitsConsumed"`
		} `json:"meta"`
	} `json:"transaction"`
	Signature string `json:"signature"`
}

type TransactionDetails string

const (
	TransactionDetailsFull       TransactionDetails = "full"
	TransactionDetailsSignatures TransactionDetails = "signatures"
	TransactionDetailsAccounts   TransactionDetails = "accounts"
	TransactionDetailsNone       TransactionDetails = "none"
)

type TransactionSubscribeFilterType struct {
	// TransactionSubscribeFilter
	Vote            *bool    `json:"vote"`
	Failed          *bool    `json:"failed"`
	Signature       string   `json:"signature"`
	AccountInclude  []string `json:"accountInclude"`
	AccountExclude  []string `json:"accountExclude"`
	AccountRequired []string `json:"accountRequired"`
}

type TransactionSubscribeOptionsType struct {
	// Optional - TransactionSubscribeOptions
	Commitment                     rpc.CommitmentType  `json:"commitment,omitempty"`
	Encoding                       solana.EncodingType `json:"encoding,omitempty"`
	TransactionDetails             TransactionDetails  `json:"transactionDetails,omitempty"`
	ShowRewards                    *bool               `json:"showRewards,omitempty"`
	MaxSupportedTransactionVersion *uint8              `json:"maxSupportedTransactionVersion,omitempty"`
}

func (c *HeliusClient) transactionSubscribe(filter TransactionSubscribeFilterType, opts TransactionSubscribeOptionsType) (*TransactionSubscription, error) {
	params := rpc.M{}
	if filter.Vote != nil {
		params["vote"] = *filter.Vote
	}
	if filter.Failed != nil {
		params["failed"] = *filter.Failed
	}
	if filter.Signature != "" {
		params["signature"] = filter.Signature
	}
	if len(filter.AccountInclude) > 0 {
		params["accountInclude"] = filter.AccountInclude
	}
	if len(filter.AccountExclude) > 0 {
		params["accountExclude"] = filter.AccountExclude
	}
	if len(filter.AccountRequired) > 0 {
		params["accountRequired"] = filter.AccountRequired
	}

	conf := rpc.M{}
	if opts.Commitment != "" {
		conf["commitment"] = opts.Commitment
	}
	if opts.Encoding != "" {
		conf["encoding"] = opts.Encoding
	}
	if opts.TransactionDetails != "" {
		conf["transactionDetails"] = opts.TransactionDetails
	}
	if opts.ShowRewards != nil {
		conf["showRewards"] = *opts.ShowRewards
	}
	if opts.MaxSupportedTransactionVersion != nil {
		conf["maxSupportedTransactionVersion"] = *opts.MaxSupportedTransactionVersion
	}

	genSub, err := c.subscribe(
		[]interface{}{params},
		conf,
		"transactionSubscribe",
		"transactionUnsubscribe",
		func(msg []byte) (interface{}, error) {
			var res TransactionResult
			err := decodeResponseFromMessage(msg, &res)
			return &res, err
		},
	)
	if err != nil {
		return nil, err
	}
	return &TransactionSubscription{
		sub: genSub,
	}, nil
}

type TransactionSubscription struct {
	sub *Subscription
}

func (sw *TransactionSubscription) Recv() (*TransactionResult, error) {
	select {
	case d := <-sw.sub.stream:
		return d.(*TransactionResult), nil
	case err := <-sw.sub.err:
		return nil, err
	}
}

func (sw *TransactionSubscription) RecvWithContext(ctx context.Context) (*TransactionResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case d := <-sw.sub.stream:
		return d.(*TransactionResult), nil
	case err := <-sw.sub.err:
		return nil, err
	}
}

func (sw *TransactionSubscription) Unsubscribe() {
	sw.sub.Unsubscribe()
}
