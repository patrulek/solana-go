// Copyright 2021 github.com/gagliardetto
// This file has been modified by github.com/gagliardetto
//
// Copyright 2020 dfuse Platform Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gagliardetto/solana-go"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"go.uber.org/zap"
)

type result interface{}

type Client struct {
	rpcURL                  string
	conn                    *websocket.Conn
	connCtx                 context.Context
	connCtxCancel           context.CancelFunc
	lock                    sync.RWMutex
	subscriptionByRequestID map[uint64]*Subscription
	subscriptionByWSSubID   map[uint64]*Subscription
	reconnectOnErr          bool
	pongWait                time.Duration
	pingPeriod              time.Duration
	subIDRetrievals         map[string]subIDRetrievalFunc
	txDiscarders            map[string]txDiscarderFunc
	sigRetrievals           map[string]signatureRetrievalFunc
	sigCache                LogsSignatureCache
}

type subIDRetrievalFunc func([]byte) (uint64, bool)
type txDiscarderFunc func([]byte) bool
type signatureRetrievalFunc func([]byte) solana.Signature

type LogsSignatureCache interface {
	Has(sig solana.Signature) bool
	Set(sig solana.Signature)
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

// Connect creates a new websocket client connecting to the provided endpoint.
func Connect(ctx context.Context, rpcEndpoint string) (c *Client, err error) {
	return ConnectWithOptions(ctx, rpcEndpoint, nil, nil)
}

// ConnectWithOptions creates a new websocket client connecting to the provided
// endpoint with a http header if available The http header can be helpful to
// pass basic authentication params as prescribed
// ref https://github.com/gorilla/websocket/issues/209
func ConnectWithOptions(ctx context.Context, rpcEndpoint string, opt *Options, cache LogsSignatureCache) (c *Client, err error) {
	c = &Client{
		rpcURL:                  rpcEndpoint,
		subscriptionByRequestID: map[uint64]*Subscription{},
		subscriptionByWSSubID:   map[uint64]*Subscription{},
		subIDRetrievals:         make(map[string]subIDRetrievalFunc),
		txDiscarders:            make(map[string]txDiscarderFunc),
		sigRetrievals:           make(map[string]signatureRetrievalFunc),
		sigCache:                &defaultLogsSignatureCache{},
	}

	dialer := &websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  DefaultHandshakeTimeout,
		EnableCompression: true,
	}

	if cache != nil {
		c.sigRetrievals = defaultSigRetrievals
		c.sigCache = cache
	}

	if opt != nil && opt.HandshakeTimeout > 0 {
		dialer.HandshakeTimeout = opt.HandshakeTimeout
	}

	if opt != nil && opt.PongWait > 0 {
		c.pongWait = opt.PongWait

		if opt.PingPeriod > 0 {
			c.pingPeriod = opt.PingPeriod
		} else {
			c.pingPeriod = (c.pongWait * 9) / 10
		}
	} else {
		c.pongWait = pongWait
		c.pingPeriod = pingPeriod
	}

	if opt != nil && opt.UseSubIDRetrievals {
		c.subIDRetrievals = defaultSubIDRetrievals
	}

	if opt != nil && opt.DiscardFailedTxs {
		c.txDiscarders = defaultTxDiscarders
	}

	var httpHeader http.Header = nil
	if opt != nil && opt.HttpHeader != nil && len(opt.HttpHeader) > 0 {
		httpHeader = opt.HttpHeader
	}
	c.conn, _, err = dialer.DialContext(ctx, rpcEndpoint, httpHeader)
	if err != nil {
		return nil, fmt.Errorf("new ws client: dial: %w", err)
	}

	c.connCtx, c.connCtxCancel = context.WithCancel(context.Background())
	go func() {
		c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
		c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(c.pongWait)); return nil })
		ticker := time.NewTicker(c.pingPeriod)
		for {
			select {
			case <-c.connCtx.Done():
				return
			case <-ticker.C:
				c.sendPing()
			}
		}
	}()
	go c.receiveMessages()
	return c, nil
}

func (c *Client) sendPing() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
		return
	}
}

func (c *Client) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.connCtxCancel()
	c.conn.Close()
}

func (c *Client) receiveMessages() {
	for {
		select {
		case <-c.connCtx.Done():
			return
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				c.closeAllSubscription(err)
				return
			}
			c.handleMessage(message)
		}
	}
}

// GetUint64 returns the value retrieved by `Get`, cast to a uint64 if possible.
// If key data type do not match, it will return an error.
func getUint64(data []byte, keys ...string) (val uint64, err error) {
	v, t, _, e := jsonparser.Get(data, keys...)
	if e != nil {
		return 0, e
	}
	if t != jsonparser.Number {
		return 0, fmt.Errorf("Value is not a number: %s", string(v))
	}
	return strconv.ParseUint(string(v), 10, 64)
}

func getUint64WithOk(data []byte, path ...string) (uint64, bool) {
	val, err := getUint64(data, path...)
	if err == nil {
		return val, true
	}
	return 0, false
}

func (c *Client) handleMessage(message []byte) {
	// when receiving message with id. the result will be a subscription number.
	// that number will be associated to all future message destine to this request
	// such message should be no longer than 128 bytes
	if len(message) < 128 {
		var result struct {
			ID     uint64 `json:"id"`
			Result uint64 `json:"result"`
		}
		jsoniter.Unmarshal(message, &result)
		if result.ID != 0 && result.Result != 0 {
			c.handleNewSubscriptionMessage(result.ID, result.Result)
			return
		}
	}

	method, err := jsonparser.GetString(message, "method")
	if err != nil {
		zlog.Warn("unable to parse ws message method", zap.Error(err))
		return
	}

	txDiscarder, discarderOk := c.txDiscarders[method]
	if discarderOk && txDiscarder(message) {
		return
	}

	sigRetrieval, sigRetrievalOk := c.sigRetrievals[method]
	if sigRetrievalOk {
		sig := sigRetrieval(message)
		if c.sigCache.Has(sig) {
			return
		}
		c.sigCache.Set(sig)
	}

	subIDRetrieval, retrievalOk := c.subIDRetrievals[method]
	if retrievalOk {
		subID, idOk := subIDRetrieval(message)
		if idOk {
			c.handleSubscriptionMessage(subID, message)
			return
		}
	}

	subID, _ := getUint64WithOk(message, "params", "subscription")
	c.handleSubscriptionMessage(subID, message)
}

func (c *Client) handleNewSubscriptionMessage(requestID, subID uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if traceEnabled {
		zlog.Debug("received new subscription message",
			zap.Uint64("message_id", requestID),
			zap.Uint64("subscription_id", subID),
		)
	}

	callBack, found := c.subscriptionByRequestID[requestID]
	if !found {
		zlog.Error("cannot find websocket message handler for a new stream.... this should not happen",
			zap.Uint64("request_id", requestID),
			zap.Uint64("subscription_id", subID),
		)
		return
	}
	callBack.subID = subID
	c.subscriptionByWSSubID[subID] = callBack

	zlog.Debug("registered ws subscription",
		zap.Uint64("subscription_id", subID),
		zap.Uint64("request_id", requestID),
		zap.Int("subscription_count", len(c.subscriptionByWSSubID)),
	)
	return
}

func (c *Client) handleSubscriptionMessage(subID uint64, message []byte) {
	if traceEnabled {
		zlog.Debug("received subscription message",
			zap.Uint64("subscription_id", subID),
		)
	}

	c.lock.RLock()
	sub, found := c.subscriptionByWSSubID[subID]
	c.lock.RUnlock()
	if !found {
		zlog.Warn("unable to find subscription for ws message", zap.Uint64("subscription_id", subID))
		return
	}

	// Decode the message using the subscription-provided decoderFunc.
	result, err := sub.decoderFunc(message)
	if err != nil {
		fmt.Println("*****************************")
		c.closeSubscription(sub.req.ID, fmt.Errorf("unable to decode client response: %w", err))
		return
	}

	// this cannot be blocking or else
	// we  will no read any other message
	if len(sub.stream) >= cap(sub.stream) {
		zlog.Warn("closing ws client subscription... not consuming fast en ought",
			zap.Uint64("request_id", sub.req.ID),
		)
		c.closeSubscription(sub.req.ID, fmt.Errorf("reached channel max capacity %d", len(sub.stream)))
		return
	}

	sub.stream <- result
	return
}

func (c *Client) closeAllSubscription(err error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, sub := range c.subscriptionByRequestID {
		sub.err <- err
	}

	c.subscriptionByRequestID = map[uint64]*Subscription{}
	c.subscriptionByWSSubID = map[uint64]*Subscription{}
}

func (c *Client) closeSubscription(reqID uint64, err error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	sub, found := c.subscriptionByRequestID[reqID]
	if !found {
		return
	}

	sub.err <- err

	err = c.unsubscribe(sub.subID, sub.unsubscribeMethod)
	if err != nil {
		zlog.Warn("unable to send rpc unsubscribe call",
			zap.Error(err),
		)
	}

	delete(c.subscriptionByRequestID, sub.req.ID)
	delete(c.subscriptionByWSSubID, sub.subID)
}

func (c *Client) unsubscribe(subID uint64, method string) error {
	req := newRequest([]interface{}{subID}, method, nil)
	data, err := req.encode()
	if err != nil {
		return fmt.Errorf("unable to encode unsubscription message for subID %d and method %s", subID, method)
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return fmt.Errorf("unable to send unsubscription message for subID %d and method %s", subID, method)
	}
	return nil
}

func (c *Client) subscribe(
	params []interface{},
	conf map[string]interface{},
	subscriptionMethod string,
	unsubscribeMethod string,
	decoderFunc decoderFunc,
) (*Subscription, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	req := newRequest(params, subscriptionMethod, conf)
	data, err := req.encode()
	if err != nil {
		return nil, fmt.Errorf("subscribe: unable to encode subsciption request: %w", err)
	}

	sub := newSubscription(
		req,
		func(err error) {
			c.closeSubscription(req.ID, err)
		},
		unsubscribeMethod,
		decoderFunc,
	)

	c.subscriptionByRequestID[req.ID] = sub
	zlog.Info("added new subscription to websocket client", zap.Int("count", len(c.subscriptionByRequestID)))

	zlog.Debug("writing data to conn", zap.String("data", string(data)))
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return nil, fmt.Errorf("unable to write request: %w", err)
	}

	return sub, nil
}

func decodeResponseFromReader(r io.Reader, reply interface{}) (err error) {
	var c *response
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return err
	}

	if c.Error != nil {
		jsonErr := &json2.Error{}
		if err := json.Unmarshal(*c.Error, jsonErr); err != nil {
			return &json2.Error{
				Code:    json2.E_SERVER,
				Message: string(*c.Error),
			}
		}
		return jsonErr
	}

	if c.Params == nil {
		return json2.ErrNullResult
	}

	return json.Unmarshal(*c.Params.Result, &reply)
}

func decodeResponseFromMessage(r []byte, reply interface{}) (err error) {
	var c *response
	if err := json.Unmarshal(r, &c); err != nil {
		return err
	}

	if c.Error != nil {
		jsonErr := &json2.Error{}
		if err := json.Unmarshal(*c.Error, jsonErr); err != nil {
			return &json2.Error{
				Code:    json2.E_SERVER,
				Message: string(*c.Error),
			}
		}
		return jsonErr
	}

	if c.Params == nil {
		return json2.ErrNullResult
	}

	return json.Unmarshal(*c.Params.Result, &reply)
}

var defaultSubIDRetrievals = map[string]subIDRetrievalFunc{
	"transactionNotification": func(b []byte) (uint64, bool) {
		// Subscription ID occurs only once and in the current Helius RPC implementation it is always at the beginning of message.
		// Use this fact to not necessarily search the whole response for the subscription ID.
		chunkEnd := 128
		if len(b) < chunkEnd {
			return 0, false
		}

		chunk := b[60:chunkEnd]
		_, after, ok := bytes.Cut(chunk, []byte(`"subscription":`))
		if !ok {
			return 0, false
		}

		idx := bytes.IndexAny(after, " ,]}")
		if idx == -1 {
			return 0, false
		}

		id := bytes.TrimSpace(after[:idx])
		subID, err := strconv.ParseUint(string(id), 10, 64)
		if err != nil {
			return 0, false
		}

		return subID, true
	},
	"logsNotification": func(b []byte) (uint64, bool) {
		// Subscription ID occurs only once and in the current Solana RPC implementation it is always at the end of message.
		// Use this fact to not necessarily search the whole response for the subscription ID.
		chunkSize := 64
		if len(b) < chunkSize {
			chunkSize = len(b)
		}

		chunk := b[len(b)-chunkSize:]
		_, after, ok := bytes.Cut(chunk, []byte(`"subscription":`))
		if !ok {
			return 0, false
		}

		// The subscription ID is a number, so we can use the `bytes.ParseUint` function.
		idx := bytes.IndexAny(after, " ,]}")
		if idx == -1 {
			return 0, false
		}

		id := bytes.TrimSpace(after[:idx])
		subID, err := strconv.ParseUint(string(id), 10, 64)
		if err != nil {
			return 0, false
		}

		return subID, true
	},
}

var defaultTxDiscarders = map[string]txDiscarderFunc{
	"logsNotification": func(b []byte) bool {
		chunkStart := 192
		chunkSize := 64

		if len(b) < chunkStart+chunkSize {
			return false
		}

		chunk := b[chunkStart : chunkStart+chunkSize]
		_, after, ok := bytes.Cut(chunk, []byte(`"err":`))
		if !ok {
			return false
		}

		idx := bytes.IndexAny(after, " ,]}")
		if idx == -1 {
			return false
		}

		value := bytes.TrimSpace(after[:idx])
		if bytes.Equal(value, []byte("null")) {
			return false
		}

		return true
	},
}

var defaultSigRetrievals = map[string]signatureRetrievalFunc{
	"logsNotification": func(b []byte) solana.Signature {
		chunkStart := 96
		chunkSize := 128

		if len(b) < chunkStart+chunkSize {
			return solana.Signature{}
		}

		chunk := b[chunkStart : chunkStart+chunkSize]
		_, after, ok := bytes.Cut(chunk, []byte(`"signature":"`))
		if !ok {
			return solana.Signature{}
		}

		idx := bytes.IndexAny(after, `"`)
		if idx == -1 {
			return solana.Signature{}
		}

		sig58 := bytes.TrimSpace(after[:idx])
		sig, _ := solana.SignatureFromBase58(string(sig58))
		return sig
	},
	"transactionNotification": func(b []byte) solana.Signature {
		chunkStart := len(b) - 128
		if chunkStart < 0 {
			return solana.Signature{}
		}

		chunk := b[chunkStart:]
		_, after, ok := bytes.Cut(chunk, []byte(`"signature":"`))
		if !ok {
			return solana.Signature{}
		}

		idx := bytes.IndexAny(after, `"`)
		if idx == -1 {
			return solana.Signature{}
		}

		sig58 := bytes.TrimSpace(after[:idx])
		sig, _ := solana.SignatureFromBase58(string(sig58))
		return sig
	},
}

type defaultLogsSignatureCache struct{}

func (c *defaultLogsSignatureCache) Has(sig solana.Signature) bool {
	return false
}

func (c *defaultLogsSignatureCache) Set(sig solana.Signature) {}
