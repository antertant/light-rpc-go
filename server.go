package lrpc

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"lrpc/codec"
	"net"
	"reflect"
	"strings"
	"sync"
)

//
// Fixed Header Definition
//
const ServeToken = 0x145ac5

// Fixed part in HTTP header to represent key information of rpc request,
// always encoded in json format
type Option struct {
	ServeToken int // rpc request is makred with corresponding ServeToken
	CodecType  codec.Type
}

var DefaultOption = &Option{
	ServeToken: ServeToken,
	CodecType:  codec.GobType,
}

//
// Server Struct
//
type Server struct {
	serviceMap sync.Map
}

type request struct {
	header       *codec.Header
	argv, replyv reflect.Value
	mtype        *methodType
	svc          *service
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

//
// Server Methods
//

func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("RPC service already registered: " + s.name)
	}
	return nil
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("can't find service: " + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("can't find method: " + methodName)
	}
	return
}

// listening to the given port and throw the received connection request to connection handler
func (server *Server) Accept(listener net.Listener) {
	for {
		connect, err := listener.Accept()
		if err != nil {
			log.Println("[RPC Server] accept error:", err)
			return
		}
		go server.ServeConnect(connect)
	}
}

func Accept(listener net.Listener) {
	DefaultServer.Accept(listener)
}

// connection handler, check the Option header and set encoder/decoder
func (server *Server) ServeConnect(connect io.ReadWriteCloser) {
	defer func() { _ = connect.Close() }()
	var option Option
	if err := json.NewDecoder(connect).Decode(&option); err != nil {
		log.Println("[RPC Server] option error:", err)
		return
	}
	if option.ServeToken != ServeToken {
		log.Println("[RPC Server] invalid serve token %x", option.ServeToken)
		return
	}
	newCodec := codec.NewCodecFuncMap[option.CodecType]
	if newCodec == nil {
		log.Println("[RPC Server] invald codec type %s", option.CodecType)
		return
	}
	server.serveCodec(newCodec(connect))
}

// placeholder for response argv when error occurs
var invalidRequest = struct{}{}

func (server *Server) serveCodec(cc codec.Codec) {
	// the action of response clients needs to be atom to avoid transmition mistakes
	sending := new(sync.Mutex)

	// multiple requests through one conenction is allowed, thus wait until all requests are handled
	waitGroup := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // impossible to recover, thus terminate
			}
			req.header.Error = err.Error()
			server.sendResponse(cc, req.header, invalidRequest, sending)
			continue
		}
		waitGroup.Add(1)
		go server.handleRequest(cc, req, sending, waitGroup)
	}
	waitGroup.Wait()
	_ = cc.Close()
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var header codec.Header
	if err := cc.ReadHeader(&header); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("[RPC Server] read header error:", err)
		}
		return nil, err
	}
	return &header, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	header, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{header: header}

	req.svc, req.mtype, err = server.findService(header.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("[RPC Server] read argv error:", err)
		return req, err
	}
	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, header *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(header, body); err != nil {
		log.Println("[RPC Server] write response error:", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	err := req.svc.call(req.mtype, req.argv, req.replyv)
	if err != nil {
		req.header.Error = err.Error()
		server.sendResponse(cc, req.header, invalidRequest, sending)
		return
	}
	server.sendResponse(cc, req.header, req.replyv.Interface(), sending)
}
