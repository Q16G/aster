// Go crypto 弱算法 / 不安全随机 / TLS 跳过验证 测试用例
package testdata

import (
	"crypto/des"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha1"
	"crypto/tls"
	"math/rand"
	"net/http"
)

// ============= 弱算法 =============

func useDES(key []byte) {
	// ruleid: go-crypto-weak-algorithm
	des.NewCipher(key)
}

func use3DES(key []byte) {
	// ruleid: go-crypto-weak-algorithm
	des.NewTripleDESCipher(key)
}

func useRC4(key []byte) {
	// ruleid: go-crypto-weak-algorithm
	rc4.NewCipher(key)
}

func useMD5(data []byte) {
	// ruleid: go-crypto-weak-algorithm
	md5.Sum(data)
}

func useSHA1(data []byte) {
	// ruleid: go-crypto-weak-algorithm
	sha1.Sum(data)
}

// ============= 不安全随机 =============

func generateToken() string {
	// ruleid: go-crypto-insecure-random
	return string(rune(rand.Intn(1000000)))
}

func generateKey() int64 {
	// ruleid: go-crypto-insecure-random
	return rand.Int63()
}

// ============= TLS 跳过验证 =============

func insecureClient() *http.Client {
	// ruleid: go-crypto-tls-insecure-skip-verify
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &http.Client{Transport: tr}
}

// ============= 安全写法 =============

// ok: go-crypto-insecure-random
// (crypto/rand 的 Read 不会被命中，因为 pattern-not-inside 排除了)
