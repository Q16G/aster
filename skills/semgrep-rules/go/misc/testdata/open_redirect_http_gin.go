package testdata

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func insecureHTTPRedirect(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-misc-open-redirect-http-gin
	http.Redirect(w, r, r.URL.Query().Get("next"), http.StatusFound)
}

func insecureHTTPRedirectAssigned(w http.ResponseWriter, r *http.Request) {
	target := r.FormValue("redirect")
	// ruleid: go-misc-open-redirect-http-gin
	http.Redirect(w, r, target, http.StatusFound)
}

func safeHTTPRedirect(w http.ResponseWriter, r *http.Request) {
	// ok: go-misc-open-redirect-http-gin
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func insecureGinRedirect(c *gin.Context) {
	// ruleid: go-misc-open-redirect-http-gin
	c.Redirect(http.StatusFound, c.Query("next"))
}

func safeGinRedirect(c *gin.Context) {
	// ok: go-misc-open-redirect-http-gin
	c.Redirect(http.StatusFound, "/profile")
}
