package utils

import (
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/sirupsen/logrus"
)

type JWT struct {
	Key interface{}
}

type UserClaims struct {
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	CreateTime time.Time `json:"create_time"`
	Authority  string    `json:"authority"`
	TenantID   string    `json:"tenant_id"`
	jwt.StandardClaims
}

func NewJWT(key interface{}) *JWT {
	return &JWT{
		Key: key,
	}
}

// 生成token
func (j *JWT) GenerateToken(claims UserClaims) (string, error) {
	claims.ExpiresAt = time.Now().Add(time.Hour * 24 * 30).Unix()
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 生成token
	return tokenClaims.SignedString(j.Key)
}

// 解析token
func (j *JWT) ParseToken(token string) (*UserClaims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &UserClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return j.Key, nil
	})
	if err != nil {
		logrus.Error(err.Error())
		return nil, err
	}
	if claims, ok := tokenClaims.Claims.(*UserClaims); ok && tokenClaims.Valid {
		return claims, nil
	}
	logrus.Error(err.Error())
	return nil, err
}
