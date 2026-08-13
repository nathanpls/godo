// Package http provides a small HTTP router and WebSocket implementation built
// on the Go standard library.
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
package http
