package middleware

import (
	"context"
	"net/http"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware คือ 'ด่านตรวจ' สำหรับยืนยันตัวตนด้วย Firebase ID Token
func AuthMiddleware(authClient *auth.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. ดึง ID Token จาก Header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
			return
		}

		// 2. ตรวจสอบรูปแบบ "Bearer <token>"
		idToken := strings.Replace(authHeader, "Bearer ", "", 1)
		if idToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization token not provided"})
			return
		}

		// 3. ตรวจสอบความถูกต้องของ Token กับ Firebase
		token, err := authClient.VerifyIDToken(context.Background(), idToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization token"})
			return
		}

		// 4. (Optional) เก็บ UID ไว้ใน Context เพื่อให้ Handler ที่อยู่ถัดไปใช้งานได้
		c.Set("uid", token.UID)

		// 5. ถ้าทุกอย่างถูกต้อง ให้คำขอเดินทางต่อไปยัง Handler หลัก
		c.Next()
	}
}
