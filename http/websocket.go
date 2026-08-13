package http

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MessageType identifies a WebSocket data message.
type MessageType byte

const (
	// TextMessage contains UTF-8 text.
	TextMessage MessageType = 1
	// BinaryMessage contains arbitrary bytes.
	BinaryMessage MessageType = 2
)

const (
	opContinuation byte = 0
	opClose        byte = 8
	opPing         byte = 9
	opPong         byte = 10

	closeNormal        = 1000
	closeProtocolError = 1002
	closeInvalidData   = 1007
	closeTooLarge      = 1009

	defaultMaxMessageSize int64 = 1 << 20
	websocketGUID               = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// WebSocketHandler handles an upgraded WebSocket connection. The connection
// is closed when the handler returns.
type WebSocketHandler func(*Conn, *stdhttp.Request)

// Upgrader controls the WebSocket handshake.
type Upgrader struct {
	// CheckOrigin permits a browser Origin. The default accepts requests without
	// an Origin and requests whose Origin host matches the request Host.
	CheckOrigin func(*stdhttp.Request) bool

	// MaxMessageSize limits a complete message, including all fragments. Zero
	// uses a 1 MiB limit.
	MaxMessageSize int64
}

// CloseError reports a close frame received from the peer.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("websocket closed: %d", e.Code)
	}
	return fmt.Sprintf("websocket closed: %d %s", e.Code, e.Reason)
}

// Conn is a server-side WebSocket connection. One goroutine may read while one
// goroutine writes. Multiple concurrent writers are serialized.
type Conn struct {
	conn           net.Conn
	reader         io.Reader
	maxMessageSize int64
	writeMu        sync.Mutex
	closeOnce      sync.Once
}

// WebSocket registers a GET route that upgrades requests to WebSockets.
func (r *Router) WebSocket(pattern string, handler WebSocketHandler) {
	r.Get(pattern, func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		conn, err := Upgrade(w, request)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn, request)
	})
}

// Upgrade upgrades an HTTP request using secure defaults. Use an Upgrader when
// a custom origin policy or message limit is needed.
func Upgrade(w stdhttp.ResponseWriter, r *stdhttp.Request) (*Conn, error) {
	return (Upgrader{}).Upgrade(w, r)
}

// Upgrade performs a WebSocket handshake and takes ownership of the underlying
// network connection. Handshake errors are written to w.
func (u Upgrader) Upgrade(w stdhttp.ResponseWriter, r *stdhttp.Request) (*Conn, error) {
	if err := u.validateRequest(r); err != nil {
		stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
		return nil, err
	}

	hijacker, ok := w.(stdhttp.Hijacker)
	if !ok {
		err := errors.New("websocket upgrade requires HTTP/1.1 hijacking support")
		stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
		return nil, err
	}

	networkConn, buffer, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack connection: %w", err)
	}

	accept := websocketAccept(r.Header.Get("Sec-WebSocket-Key"))
	if _, err = fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err == nil {
		err = buffer.Flush()
	}
	if err != nil {
		networkConn.Close()
		return nil, fmt.Errorf("write websocket handshake: %w", err)
	}

	limit := u.MaxMessageSize
	if limit == 0 {
		limit = defaultMaxMessageSize
	}
	return &Conn{conn: networkConn, reader: buffer.Reader, maxMessageSize: limit}, nil
}

func (u Upgrader) validateRequest(r *stdhttp.Request) error {
	if r.Method != stdhttp.MethodGet {
		return errors.New("websocket upgrade requires GET")
	}
	if !headerContains(r.Header, "Connection", "upgrade") || !headerContains(r.Header, "Upgrade", "websocket") {
		return errors.New("missing websocket upgrade headers")
	}
	if !headerContains(r.Header, "Sec-WebSocket-Version", "13") {
		return errors.New("unsupported websocket version")
	}

	key, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
	if err != nil || len(key) != 16 {
		return errors.New("invalid Sec-WebSocket-Key")
	}

	checkOrigin := u.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = sameOrigin
	}
	if !checkOrigin(r) {
		return errors.New("websocket origin not allowed")
	}
	if u.MaxMessageSize < 0 {
		return errors.New("MaxMessageSize must not be negative")
	}
	return nil
}

func sameOrigin(r *stdhttp.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

func headerContains(header stdhttp.Header, name, value string) bool {
	for _, line := range header.Values(name) {
		for token := range strings.SplitSeq(line, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return true
			}
		}
	}
	return false
}

func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// ReadMessage waits for the next text or binary message. Ping frames are
// answered automatically and pong frames are ignored.
func (c *Conn) ReadMessage() (MessageType, []byte, error) {
	var message bytes.Buffer
	var messageType MessageType
	fragmented := false

	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch opcode {
		case byte(TextMessage), byte(BinaryMessage):
			if fragmented {
				return 0, nil, c.protocolError("new data frame before fragmented message completed")
			}
			messageType = MessageType(opcode)
			fragmented = !fin
		case opContinuation:
			if !fragmented {
				return 0, nil, c.protocolError("continuation frame without fragmented message")
			}
			fragmented = !fin
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			closeErr, err := parseClose(payload)
			if err != nil {
				return 0, nil, c.protocolError(err.Error())
			}
			_ = c.writeFrame(opClose, payload)
			return 0, nil, closeErr
		default:
			return 0, nil, c.protocolError("unknown opcode")
		}

		if int64(message.Len())+int64(len(payload)) > c.maxMessageSize {
			_ = c.writeClose(closeTooLarge, "message too large")
			return 0, nil, errors.New("websocket message exceeds read limit")
		}
		message.Write(payload)
		if fragmented {
			continue
		}

		data := message.Bytes()
		if messageType == TextMessage && !utf8.Valid(data) {
			_ = c.writeClose(closeInvalidData, "invalid UTF-8")
			return 0, nil, errors.New("websocket text message is not valid UTF-8")
		}
		return messageType, data, nil
	}
}

func (c *Conn) readFrame() (bool, byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return false, 0, nil, err
	}

	fin := header[0]&0x80 != 0
	opcode := header[0] & 0x0f
	if header[0]&0x70 != 0 {
		return false, 0, nil, c.protocolError("reserved frame bits are set")
	}
	if header[1]&0x80 == 0 {
		return false, 0, nil, c.protocolError("client frame is not masked")
	}

	payloadLength := uint64(header[1] & 0x7f)
	switch payloadLength {
	case 126:
		var length [2]byte
		if _, err := io.ReadFull(c.reader, length[:]); err != nil {
			return false, 0, nil, err
		}
		payloadLength = uint64(binary.BigEndian.Uint16(length[:]))
		if payloadLength < 126 {
			return false, 0, nil, c.protocolError("non-minimal frame length")
		}
	case 127:
		var length [8]byte
		if _, err := io.ReadFull(c.reader, length[:]); err != nil {
			return false, 0, nil, err
		}
		payloadLength = binary.BigEndian.Uint64(length[:])
		if payloadLength < 65536 || payloadLength>>63 != 0 {
			return false, 0, nil, c.protocolError("invalid frame length")
		}
	}

	isControl := opcode&0x08 != 0
	if isControl && (!fin || payloadLength > 125) {
		return false, 0, nil, c.protocolError("invalid control frame")
	}
	if payloadLength > uint64(c.maxMessageSize) || payloadLength > uint64(maxInt()) {
		_ = c.writeClose(closeTooLarge, "message too large")
		return false, 0, nil, errors.New("websocket frame exceeds read limit")
	}

	var mask [4]byte
	if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
		return false, 0, nil, err
	}
	payload := make([]byte, int(payloadLength))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return false, 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%len(mask)]
	}
	return fin, opcode, payload, nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func parseClose(payload []byte) (*CloseError, error) {
	if len(payload) == 0 {
		return &CloseError{Code: closeNormal}, nil
	}
	if len(payload) == 1 {
		return nil, errors.New("close frame has an invalid payload")
	}

	code := int(binary.BigEndian.Uint16(payload[:2]))
	if !validCloseCode(code) {
		return nil, errors.New("close frame has an invalid status code")
	}
	reason := payload[2:]
	if !utf8.Valid(reason) {
		return nil, errors.New("close reason is not valid UTF-8")
	}
	return &CloseError{Code: code, Reason: string(reason)}, nil
}

func validCloseCode(code int) bool {
	if code >= 3000 && code <= 4999 {
		return true
	}
	switch code {
	case 1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011, 1012, 1013, 1014:
		return true
	default:
		return false
	}
}

func (c *Conn) protocolError(reason string) error {
	_ = c.writeClose(closeProtocolError, reason)
	return errors.New("websocket protocol error: " + reason)
}

// WriteMessage sends a complete text or binary message.
func (c *Conn) WriteMessage(messageType MessageType, payload []byte) error {
	if messageType != TextMessage && messageType != BinaryMessage {
		return errors.New("invalid websocket message type")
	}
	if messageType == TextMessage && !utf8.Valid(payload) {
		return errors.New("websocket text message is not valid UTF-8")
	}
	return c.writeFrame(byte(messageType), payload)
}

// ReadJSON reads the next text or binary message and decodes its JSON value.
func (c *Conn) ReadJSON(value any) error {
	_, payload, err := c.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}

// WriteJSON encodes value and sends it as a text message.
func (c *Conn) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteMessage(TextMessage, payload)
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	header := []byte{0x80 | opcode}
	switch length := len(payload); {
	case length <= 125:
		header = append(header, byte(length))
	case uint64(length) <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[len(header)-8:], uint64(length))
	}

	if err := writeAll(c.conn, header); err != nil {
		return err
	}
	return writeAll(c.conn, payload)
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}

func (c *Conn) writeClose(code int, reason string) error {
	if len(reason) > 123 {
		reason = reason[:123]
	}
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], reason)
	return c.writeFrame(opClose, payload)
}

// SetReadDeadline sets the deadline for future reads.
func (c *Conn) SetReadDeadline(deadline time.Time) error {
	return c.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline sets the deadline for future writes.
func (c *Conn) SetWriteDeadline(deadline time.Time) error {
	return c.conn.SetWriteDeadline(deadline)
}

// Close sends a normal close frame and closes the network connection.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		_ = c.writeClose(closeNormal, "")
		err = c.conn.Close()
	})
	return err
}
