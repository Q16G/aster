// html/template 误用测试用例
package testdata

import (
	"html/template"
	"net/http"
	text_template "text/template"
)

// ============= 信任类型强转（HIGH） =============

type Page struct {
	Body template.HTML
	Code template.JS
	Link template.URL
}

func renderUserHTML(w http.ResponseWriter, r *http.Request) {
	body := r.URL.Query().Get("body")
	// ruleid: go-xss-html-template-trusted-type
	p := Page{Body: template.HTML(body)}

	t, _ := template.ParseFiles("page.html")
	t.Execute(w, p)
}

func renderUserJS(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	// ruleid: go-xss-html-template-trusted-type
	p := Page{Code: template.JS(code)}
	_ = p
}

func renderUserURL(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	// ruleid: go-xss-html-template-trusted-type
	p := Page{Link: template.URL(u)}
	_ = p
}

// ============= text/template 加载 .html =============

func renderTextAsHTML(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-xss-text-template-as-html
	t, _ := text_template.ParseFiles("page.html")
	t.Execute(w, r.URL.Query().Get("name"))
}

func renderTextGlob(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-xss-text-template-as-html
	t, _ := text_template.ParseGlob("*.tmpl")
	t.Execute(w, r.URL.Query().Get("name"))
}

// ============= 安全写法（不应被命中） =============

func safeLiteralHTML(w http.ResponseWriter, r *http.Request) {
	// ok: go-xss-html-template-trusted-type
	p := Page{Body: template.HTML("<p>static content</p>")}
	_ = p
}

func safePlainString(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	t, _ := template.ParseFiles("page.html")
	// ok: go-xss-html-template-trusted-type
	t.Execute(w, name) // 普通 string 走 html/template 自动转义
}
