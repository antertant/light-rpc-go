// Asynchronous and concurrent supported client

package lrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"lrpc/codec"
	"net"
	"sync"
	"time"
)

//
//	Structs and Variables
//

// information for calling a method
type Call struct {
	Seq           uint64
	ServiceMethod string      // format: "<service>.<method>"
	Args          interface{} // arguments for the called methods
	Reply         interface{} // return value of the called methods
	Error         error
	Done          chan *Call // informs the service invoker the calling is completed
}

func (call *Call) done() {
	call.Done <- call
}

// client can have multiple calls; process can have multiple client goroutines
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex // gurantees the atom response procedure
	header   codec.Header
	mu       sync.Mutex // protect data for current client since there are multiple client goroutines
	seq      uint64
	pending  map[uint64]*Call // a client can call multiple methods asynchronously. Key: call seq
	closing  bool             // user has called close
	shutdown bool             // server has called close
}

type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection has shut down")

//
//	Methods
//

// connection close handler
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing { // client cannot be closed twice
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !(client.closing || client.shutdown)
}

//
// Methods related to remote methods call.
// 			Add, Remove, Clear
//

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

//
//	Methods Call Transmission
//

func (client *Client) receive() {
	var err error
	for err == nil {
		// client received message saved in <h>
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.SeqId)
		switch {
		case call == nil: // call is unregistered
			err = client.cc.ReadBody(nil)
		case h.Error != "": // there is error in server
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default: // call is completed, remove from queue
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body" + err.Error())
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}

func (client *Client) send(call *Call) {
	// guarantee the atom transmission
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// request header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.SeqId = seq
	client.header.Error = ""

	if err := client.cc.Write(&client.header, call.Args); err != nil {
		// transmission failed, rollback
		call := client.removeCall(seq)
		// if <call> is nil, then the client has handled the wrong call
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("[RPC Client] done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call invokes <serviceMethod>, waits for it to complete, then returns its error status
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done(): // using context.WithTimeout to control the timeout
		client.removeCall(call.Seq)
		return errors.New("call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

//
//	Server-Client Connection Establishment
//

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("[RPC Client] codec error:", err)
		return nil, err
	}
	// send option
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("[RPC Client] options error:", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     1,
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func parseOptions(opts ...*Option) (*Option, error) {
	// lose parameter or invalid parameter
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("cannot accpet more than 1 options")
	}
	opt := opts[0]
	opt.ServeToken = DefaultOption.ServeToken
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// connect to an RPC server at <address>
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	return dialTimeout(NewClient, network, address, opts...)
}

func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("[RPC Client] connect timeout: expect result within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}
