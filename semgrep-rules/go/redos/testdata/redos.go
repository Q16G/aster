// Go ReDoS / 用户控制的正则模式 测试用例
package testdata

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
)

// ============= net/http =============

func compileFromQuery(w http.ResponseWriter, r *http.Request) {
	pat := r.URL.Query().Get("pat")
	// ruleid: go-redos-tainted-pattern
	re, _ := regexp.Compile(pat)
	_ = re
}

func mustCompileFromForm(w http.ResponseWriter, r *http.Request) {
	pat := r.FormValue("pat")
	// ruleid: go-redos-tainted-pattern
	_ = regexp.MustCompile(pat)
}

func matchStringFromHeader(w http.ResponseWriter, r *http.Request) {
	pat := r.Header.Get("X-Pattern")
	// ruleid: go-redos-tainted-pattern
	regexp.MatchString(pat, "abc")
}

// ============= gin =============

func ginCompile(c *gin.Context) {
	pat := c.Query("pat")
	// ruleid: go-redos-tainted-pattern
	_ = regexp.MustCompile(pat)
}

// ============= 安全写法（不应被命中） =============

var staticRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func staticPattern(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query().Get("v")
	// ok: go-redos-tainted-pattern
	staticRE.MatchString(v)
}

func literalPattern(w http.ResponseWriter, r *http.Request) {
	// ok: go-redos-tainted-pattern
	regexp.MustCompile(`^\d+$`)
}
