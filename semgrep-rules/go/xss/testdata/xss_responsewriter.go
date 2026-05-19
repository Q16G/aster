// Go XSS 测试用例：直接把用户输入写入 HTTP 响应
// 仅用于 semgrep 规则验证
package testdata

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ============= net/http =============

func httpWrite(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	// ruleid: go-xss-responsewriter
	w.Write([]byte("<h1>Hello " + name + "</h1>"))
}

func httpFprintf(w http.ResponseWriter, r *http.Request) {
	q := r.FormValue("q")
	// ruleid: go-xss-responsewriter
	fmt.Fprintf(w, "<p>%s</p>", q)
}

func httpIoWriteString(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("X-Note")
	// ruleid: go-xss-responsewriter
	io.WriteString(w, h)
}

func httpFprintln(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-xss-responsewriter
	fmt.Fprintln(w, r.URL.RawQuery)
}

// ============= gin =============

func ginHTML(c *gin.Context) {
	name := c.Query("name")
	// ruleid: go-xss-responsewriter
	c.HTML(http.StatusOK, "index.tmpl", name)
}

func ginString(c *gin.Context) {
	name := c.Param("name")
	// ruleid: go-xss-responsewriter
	c.String(http.StatusOK, "Hello %s", name)
}

// ============= 安全写法（不应被命中） =============

func safeEscape(w http.ResponseWriter, r *http.Request) {
	name := html.EscapeString(r.URL.Query().Get("name"))
	// ok: go-xss-responsewriter
	fmt.Fprintf(w, "<p>%s</p>", name)
}

func safeAtoi(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	// ok: go-xss-responsewriter
	fmt.Fprintf(w, "id=%d", id)
}

func safeJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// ok: go-xss-responsewriter
	w.Write([]byte(`{"ok":true}`))
}
