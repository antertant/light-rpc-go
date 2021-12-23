package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	connect         io.ReadWriteCloser
	decoder         *gob.Decoder
	encoder         *gob.Encoder
	bufferedWritter *bufio.Writer
}

// make sure all methods in the interface is implemented
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(connect io.ReadWriteCloser) Codec {
	bufferedWritter := bufio.NewWriter(connect)
	return &GobCodec{
		connect:         connect,
		decoder:         gob.NewDecoder(connect),
		encoder:         gob.NewEncoder(bufferedWritter),
		bufferedWritter: bufferedWritter,
	}
}

func (c *GobCodec) ReadHeader(header *Header) error {
	return c.decoder.Decode(header)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.decoder.Decode(body)
}

func (c *GobCodec) Write(header *Header, body interface{}) (err error) {
	defer func() {
		_ = c.bufferedWritter.Flush()
		if err != nil {
			_ = c.Close
		}
	}()
	if err := c.encoder.Encode(header); err != nil {
		log.Println("[RPC Codec Encoding error] cannot encode header under gob:", err)
		return err
	}
	if err := c.encoder.Encode(body); err != nil {
		log.Println("[RPC Codec Encoding error] cannot encode body under gob:", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.connect.Close()
}
