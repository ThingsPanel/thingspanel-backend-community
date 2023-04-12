package utils

import (
	"ThingsPanel-Go/models"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/dgrijalva/jwt-go"
)

type UserClaims struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreateTime time.Time `json:"create_time"`
	jwt.StandardClaims
}

// 生成jwt的token
func MakeCliamsToken(o UserClaims) (string, error) {
	jwt_secret, _ := beego.AppConfig.String("jwt_secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, o)
	tokenString, err := token.SignedString([]byte(jwt_secret))
	return tokenString, err
}

// 解密jwt的token
func ParseCliamsToken(token string) (*UserClaims, error) {
	jwt_secret, _ := beego.AppConfig.String("jwt_secret")
	tokenClaims, err := jwt.ParseWithClaims(token, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(jwt_secret), nil
	})
	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*UserClaims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}
	return nil, err
}

// 生成token,并返回token,过期时间1小时
func GenerateToken(user *models.Users) (string, error) {
	claims := UserClaims{
		ID:         user.ID,
		Name:       user.Email,
		CreateTime: time.Now(),
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}, //过期时间1小时
	}
	return MakeCliamsToken(claims)
}
