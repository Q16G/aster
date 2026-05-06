package testdata

import (
	"mime/multipart"
	"path/filepath"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func insecureCors() {
	// ruleid: go-cors-wildcard-credentials
	_ = cors.Config{AllowAllOrigins: true, AllowCredentials: true}
}

func safeCors() {
	// ok: go-cors-wildcard-credentials
	_ = cors.Config{AllowOrigins: []string{"https://example.com"}, AllowCredentials: true}
}

func insecureSecret(token *jwt.Token) error {
	// ruleid: go-hardcoded-jwt-secret
	_, _ = jwt.Parse("raw-token", func(token *jwt.Token) (interface{}, error) { return []byte("super-secret"), nil })
	// ruleid: go-hardcoded-jwt-secret
	_, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1"}).SignedString([]byte("super-secret"))
	return err
}

func safeSecret(secret []byte) error {
	// ok: go-hardcoded-jwt-secret
	_, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1"}).SignedString(secret)
	return err
}

func insecureUpload(c *gin.Context, f *multipart.FileHeader, dir string) error {
	// ruleid: go-upload-filename-path
	return c.SaveUploadedFile(f, filepath.Join(dir, f.Filename))
}

func safeUpload(c *gin.Context, f *multipart.FileHeader, dir string) error {
	// ok: go-upload-filename-path
	return c.SaveUploadedFile(f, filepath.Join(dir, filepath.Base(f.Filename)))
}
