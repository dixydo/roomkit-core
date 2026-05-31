package server

import "net/http"

// withCORS wraps an http.Handler to emit permissive CORS headers needed for
// the JS SDK to call /api/* from a different origin (e.g. customer site).
//
// If allowedOrigins is non-empty we echo the request Origin when it's in the
// list (Vary: Origin). If empty (dev / open mode) we emit Access-Control-
// Allow-Origin: *.
func withCORS(allowedOrigins []string, next http.Handler) http.Handler {
	allowSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowSet[o] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if len(allowedOrigins) == 0 {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowSet[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
