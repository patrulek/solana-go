package ws

type HeliusClient struct {
	*Client
}

func (c *HeliusClient) TransactionSubscribe(filter TransactionSubscribeFilterType, opts TransactionSubscribeOptionsType) (*TransactionSubscription, error) {
	return c.transactionSubscribe(filter, opts)
}
