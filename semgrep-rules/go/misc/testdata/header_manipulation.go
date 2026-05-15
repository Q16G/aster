package testdata

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func insecureLocationHeader(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-misc-header-manipulation
	w.Header().Set("Location", r.RequestURI)
}

func insecureCustomHeader(w http.ResponseWriter, r *http.Request) {
	val := r.URL.Query().Get("trace")
	// ruleid: go-misc-header-manipulation
	w.Header().Add("X-Trace", val)
}

func insecureGinHeader(c *gin.Context) {
	// ruleid: go-misc-header-manipulation
	c.Header("Content-Disposition", c.Query("filename"))
}

func safeHeader(w http.ResponseWriter, r *http.Request) {
	safeValue := strings.ReplaceAll(r.URL.Query().Get("trace"), "\n", "")
	// ok: go-misc-header-manipulation
	w.Header().Set("X-Trace", safeValue)
}
