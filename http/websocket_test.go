package http

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWebSocket(t *testing.T) {
	router := New()
	router.WebSocket("/socket", func(conn *Conn, _ *stdhttp.Request) {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(messageType, message)
	})

	server := httptest.NewServer(router)
	defer server.Close()

	conn, reader := openWebSocket(t, server.URL, "/socket")
	defer conn.Close()

	writeClientFrame(t, conn, false, byte(TextMessage), []byte("hel"))
	writeClientFrame(t, conn, true, opPing, []byte("check"))
	writeClientFrame(t, conn, true, opContinuation, []byte("lo"))

	fin, opcode, payload := readServerFrame(t, reader)
	if !fin || opcode != opPong || string(payload) != "check" {
		t.Fatalf("pong frame = (%t, %d, %q)", fin, opcode, payload)
	}

	fin, opcode, payload = readServerFrame(t, reader)
	if !fin || opcode != byte(TextMessage) || string(payload) != "hello" {
		t.Fatalf("message frame = (%t, %d, %q)", fin, opcode, payload)
	}
}

func TestWebSocketJSON(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	conn := &Conn{conn: serverConn, reader: serverConn, maxMessageSize: defaultMaxMessageSize}
	result := make(chan error, 1)
	go func() {
		var input struct {
			Name string `json:"name"`
		}
		if err := conn.ReadJSON(&input); err != nil {
			result <- err
			return
		}
		result <- conn.WriteJSON(map[string]string{"hello": input.Name})
	}()

	writeClientFrame(t, clientConn, true, byte(TextMessage), []byte(`{"name":"godo"}`))
	_, opcode, payload := readServerFrame(t, bufio.NewReader(clientConn))
	if opcode != byte(TextMessage) {
		t.Fatalf("opcode = %d, want %d", opcode, TextMessage)
	}

	var response map[string]string
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	if response["hello"] != "godo" {
		t.Fatalf("response = %v", response)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketRejectsCrossOriginRequest(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "http://example.com/socket", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	request.Header.Set("Origin", "https://other.example")
	response := httptest.NewRecorder()

	if _, err := Upgrade(response, request); err == nil {
		t.Fatal("Upgrade succeeded for a cross-origin request")
	}
	if response.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusBadRequest)
	}
}

func openWebSocket(t *testing.T, serverURL, path string) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, parsed.Host, key)
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(conn)
	response, err := stdhttp.ReadResponse(reader, &stdhttp.Request{Method: stdhttp.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != stdhttp.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.StatusCode, stdhttp.StatusSwitchingProtocols)
	}
	if got, want := response.Header.Get("Sec-WebSocket-Accept"), "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="; got != want {
		t.Fatalf("Sec-WebSocket-Accept = %q, want %q", got, want)
	}
	return conn, reader
}

func writeClientFrame(t *testing.T, writer io.Writer, fin bool, opcode byte, payload []byte) {
	t.Helper()
	if len(payload) > 125 {
		t.Fatal("test helper only supports short frames")
	}

	first := opcode
	if fin {
		first |= 0x80
	}
	mask := [4]byte{1, 2, 3, 4}
	frame := []byte{first, 0x80 | byte(len(payload))}
	frame = append(frame, mask[:]...)
	for i, value := range payload {
		frame = append(frame, value^mask[i%len(mask)])
	}
	if _, err := writer.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func readServerFrame(t *testing.T, reader *bufio.Reader) (bool, byte, []byte) {
	t.Helper()

	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		t.Fatal(err)
	}
	if header[1]&0x80 != 0 {
		t.Fatal("server frame is masked")
	}

	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var value [2]byte
		if _, err := io.ReadFull(reader, value[:]); err != nil {
			t.Fatal(err)
		}
		length = uint64(binary.BigEndian.Uint16(value[:]))
	case 127:
		var value [8]byte
		if _, err := io.ReadFull(reader, value[:]); err != nil {
			t.Fatal(err)
		}
		length = binary.BigEndian.Uint64(value[:])
	}
	if length > 1024 {
		t.Fatalf("unexpected test frame length: %d", length)
	}

	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return header[0]&0x80 != 0, header[0] & 0x0f, payload
}

func TestHeaderContains(t *testing.T) {
	header := stdhttp.Header{"Connection": {"keep-alive, Upgrade"}}
	if !headerContains(header, "Connection", "upgrade") {
		t.Fatal("headerContains did not find a case-insensitive token")
	}
	if headerContains(header, "Connection", strings.Repeat("x", 3)) {
		t.Fatal("headerContains found an absent token")
	}
}
