// Go JWT algorithm-confusion / 缺失 claim 校验 测试用例
// 仅用于 semgrep 规则验证
package testdata

import (
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

var rsaPubKey *rsa.PublicKey

// ============= algorithm confusion: HIGH =============

func parseUnchecked(tokenString string) (*jwt.Token, error) {
	// ruleid: go-jwt-algorithm-confusion
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return rsaPubKey, nil
	})
}

func parseClaimsUnchecked(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	// ruleid: go-jwt-algorithm-confusion
	return jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		fmt.Println("parsing")
		return rsaPubKey, nil
	})
}

// ============= 安全写法（不应被命中） =============

func parseChecked(tokenString string) (*jwt.Token, error) {
	// ok: go-jwt-algorithm-confusion
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return rsaPubKey, nil
	})
}

func parseCheckedHMAC(tokenString string, secret []byte) (*jwt.Token, error) {
	// ok: go-jwt-algorithm-confusion
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
}

// ============= missing claim validation =============

type myClaims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
}

func parseAndUseClaims(tokenString string) string {
	// ruleid: go-jwt-missing-claim-validation
	tok, _ := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return rsaPubKey, nil
	})
	if tok != nil && tok.Valid {
		return "ok"
	}
	return ""
}

// ============= claim 校验完整：不应被命中 =============

func parseWithIssuerAudience(tokenString string) (*jwt.Token, error) {
	parser := jwt.NewParser(
		jwt.WithIssuer("https://issuer.example.com"),
		jwt.WithAudience("my-api"),
	)
	// ok: go-jwt-missing-claim-validation
	return parser.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return rsaPubKey, nil
	})
}
