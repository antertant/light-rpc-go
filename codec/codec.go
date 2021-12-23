package codec

import "io"

type Header struct {
	ServiceMethod string // "Service.Method"
	SeqId         uint64 // Sequence Number
	Error         string
}

// encoding and decoding message body
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

// constructor function
type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

// define 2 types of constructors
const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

// using a map to store constructors for the 2 types
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
	NewCodecFuncMap[JsonType] = NewJsonCodec
}
