package testdata

import (
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

type BeegoController struct{}

func (c *BeegoController) SaveToFile(field string, path string) error { return nil }

func insecureBeego(ctrl *BeegoController, header *multipart.FileHeader, dir string) error {
	// ruleid: go-upload-beego-gin-path
	return ctrl.SaveToFile("file", filepath.Join(dir, header.Filename))
}

func safeBeego(ctrl *BeegoController, header *multipart.FileHeader, dir string) error {
	// ok: go-upload-beego-gin-path
	return ctrl.SaveToFile("file", filepath.Join(dir, filepath.Base(header.Filename)))
}

func insecureMultipart(r *http.Request, dir string) error {
	_, header, err := r.FormFile("file")
	if err != nil {
		return err
	}
	// ruleid: go-upload-beego-gin-path
	_, err = os.Create(filepath.Join(dir, header.Filename))
	return err
}
