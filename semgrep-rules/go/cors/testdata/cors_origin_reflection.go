package testdata

import "net/http"

// ruleid: go-cors-origin-reflection
func reflectOriginInline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
}

// ruleid: go-cors-origin-reflection
func reflectOriginVariable(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
}

// ok: go-cors-origin-reflection
func staticOrigin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "https://trusted.example.com")
}

// ok: go-cors-origin-reflection
func wildcardOrigin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}
