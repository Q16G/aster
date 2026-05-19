// Go XML 不安全实体 / Strict 配置测试用例
package testdata

import (
	"encoding/xml"
	"io"
	"net/http"
)

// ============= Decoder.Entity 自定义实体表 =============

func decodeWithUserEntity(r *http.Request) {
	d := xml.NewDecoder(r.Body)
	// ruleid: go-xml-decoder-entity-tainted
	d.Entity = map[string]string{
		"company": "Acme Corp",
	}
	var v any
	d.Decode(&v)
}

func decodeWithCustomEntity(body io.Reader) {
	d := xml.NewDecoder(body)
	custom := map[string]string{"x": "y"}
	// ruleid: go-xml-decoder-entity-tainted
	d.Entity = custom
}

// ============= Strict 关闭 =============

func decodeNonStrict(r *http.Request) {
	d := xml.NewDecoder(r.Body)
	// ruleid: go-xml-decoder-strict-disabled
	d.Strict = false
	var v any
	d.Decode(&v)
}

// ============= 安全写法（不应被命中） =============

func decodeDefault(r *http.Request) {
	d := xml.NewDecoder(r.Body)
	// ok: go-xml-decoder-entity-tainted
	// ok: go-xml-decoder-strict-disabled
	var v any
	d.Decode(&v)
}

func decodeHTMLEntities(r *http.Request) {
	d := xml.NewDecoder(r.Body)
	// ok: go-xml-decoder-entity-tainted
	d.Entity = xml.HTMLEntity
	var v any
	d.Decode(&v)
}

func decodeReset(r *http.Request) {
	d := xml.NewDecoder(r.Body)
	// ok: go-xml-decoder-entity-tainted
	d.Entity = nil
}
