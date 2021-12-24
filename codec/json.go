package codec

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
)

type JsonCodec struct {
	connect         io.ReadWriteCloser
	decoder         *json.Decoder
	encoder         *json.Encoder
	bufferedWritter *bufio.Writer
}

// make sure all methods in the interface is implemented
var _ Codec = (*JsonCodec)(nil)

func NewJsonCodec(connect io.ReadWriteCloser) Codec {
	bufferedWritter := bufio.NewWriter(connect)
	return &JsonCodec{
		connect:         connect,
		decoder:         json.NewDecoder(connect),
		encoder:         json.NewEncoder(bufferedWritter),
		bufferedWritter: bufferedWritter,
	}
}

func (c *JsonCodec) ReadHeader(header *Header) error {
	return c.decoder.Decode(header)
}

func (c *JsonCodec) ReadBody(body interface{}) error {
	return c.decoder.Decode(body)
}

func (c *JsonCodec) Write(header *Header, body interface{}) (err error) {
	defer func() {
		_ = c.bufferedWritter.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err := c.encoder.Encode(header); err != nil {
		log.Println("[RPC Codec] cannot encode header under json:", err)
		return err
	}
	if err := c.encoder.Encode(body); err != nil {
		log.Println("[RPC Codec] cannot encode body under json:", err)
		return err
	}
	return nil
}

func (c *JsonCodec) Close() error {
	return c.connect.Close()
}
