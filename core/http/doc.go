// Package http provides routing, strict JSON requests, API responses, and
// WebSockets built on the Go standard library.
//
// A Router accepts the standard net/http handler types, so applications can use
// existing middleware and handlers without adapters. Route patterns use
// [net/http.ServeMux] syntax, including named path parameters:
//
//	router := http.New()
//	router.Get("/users/{id}", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
//		id := r.PathValue("id")
//		_ = http.JSON(w, stdhttp.StatusOK, map[string]string{"id": id})
//	})
//
//	stdhttp.ListenAndServe(":8080", router)
//
// WebSocket routes perform the handshake and close the connection when the
// handler returns:
//
//	router.WebSocket("/events", func(conn *http.Conn, r *stdhttp.Request) {
//		for {
//			messageType, message, err := conn.ReadMessage()
//			if err != nil {
//				return
//			}
//			if err := conn.WriteMessage(messageType, message); err != nil {
//				return
//			}
//		}
//	})
//
// DialWebSocket opens client connections to ws or wss URLs. Client writes are
// masked and server frames are validated according to RFC 6455.
package http
