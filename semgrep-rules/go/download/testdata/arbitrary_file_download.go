// Go 文件下载 / 任意文件读取漏洞测试用例
// 仅用于 semgrep 规则验证
package testdata

import (
	"archive/zip"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ============= net/http =============

func httpQuery(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("file")
	// ruleid: go-download-arbitrary-file
	data, _ := os.ReadFile(filepath.Join("/var/data", name))
	w.Write(data)
}

func httpFormValue(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-download-arbitrary-file
	f, _ := os.Open(r.FormValue("file"))
	defer f.Close()
}

func httpHeader(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-download-arbitrary-file
	data, _ := ioutil.ReadFile(r.Header.Get("X-File"))
	w.Write(data)
}

func httpServeFile(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-download-arbitrary-file
	http.ServeFile(w, r, r.URL.Query().Get("path"))
}

// ============= gin =============

func ginQuery(c *gin.Context) {
	// ruleid: go-download-arbitrary-file
	c.File(c.Query("file"))
}

func ginParam(c *gin.Context) {
	// ruleid: go-download-arbitrary-file
	c.FileAttachment(c.Param("name"), "download.bin")
}

func ginPostForm(c *gin.Context) {
	name := c.PostForm("name")
	// ruleid: go-download-arbitrary-file
	c.File("/var/data/" + name)
}

func ginGetHeader(c *gin.Context) {
	name := c.GetHeader("X-File")
	// ruleid: go-download-arbitrary-file
	data, _ := os.ReadFile(name)
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// ============= 压缩 / 模板 =============

func zipOpen(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-download-arbitrary-file
	z, _ := zip.OpenReader(r.URL.Query().Get("f"))
	defer z.Close()
}

func tplLoad(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("tpl")
	// ruleid: go-download-arbitrary-file
	t, _ := template.ParseFiles(name)
	t.Execute(w, nil)
}

// ============= 子进程读取 =============

func subprocessCat(w http.ResponseWriter, r *http.Request) {
	// ruleid: go-download-arbitrary-file
	out, _ := exec.Command("cat", r.URL.Query().Get("f")).Output()
	w.Write(out)
}

// ============= 命令行 / 环境变量 =============

func fromArgs() []byte {
	// ruleid: go-download-arbitrary-file
	data, _ := os.ReadFile(os.Args[1])
	return data
}

func fromEnv() []byte {
	// ruleid: go-download-arbitrary-file
	data, _ := os.ReadFile(os.Getenv("FILE_PATH"))
	return data
}

// ============= 安全写法（不应被命中） =============

func safeBase(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("file"))
	// ok: go-download-arbitrary-file
	data, _ := os.ReadFile(filepath.Join("/var/data", name))
	w.Write(data)
}

func safeInt(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	// ok: go-download-arbitrary-file
	data, _ := os.ReadFile("/var/data/" + strconv.Itoa(id) + ".bin")
	w.Write(data)
}

func safeFixed(w http.ResponseWriter, r *http.Request) {
	// ok: go-download-arbitrary-file
	data, _ := os.ReadFile("/var/data/report.csv")
	w.Write(data)
}

func safeCustom(w http.ResponseWriter, r *http.Request) {
	name := sanitizePath(r.URL.Query().Get("file"))
	// ok: go-download-arbitrary-file
	data, _ := os.ReadFile(filepath.Join("/var/data", name))
	w.Write(data)
}

func sanitizePath(p string) string {
	if filepath.IsAbs(p) {
		return ""
	}
	return p
}
