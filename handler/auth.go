package handler

import (
	"log"
	"net/http"

	// Import เพิ่ม
	"api-flash-dash/model"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	FirestoreClient *firestore.Client
	AuthClient      *auth.Client
}

// RegisterHandler สำหรับสมัครสมาชิก
func (h *AuthHandler) RegisterHandler(c *gin.Context) {
	// ใน Model อาจจะปรับแก้ให้รับแค่ Phone ไม่ต้องรับ Email
	var newUser model.User
	if err := c.ShouldBindJSON(&newUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// --- ส่วนที่แก้ไข ---
	// สร้างอีเมลแฝงจากเบอร์โทร
	// ตัวอย่าง: "0812345678" -> "0812345678@flashdash.app"
	// คุณสามารถเปลี่ยน "flashdash.app" เป็นชื่อโดเมนของแอปคุณได้
	syntheticEmail := newUser.Phone + "@flashdash.app"

	// 1. สร้างผู้ใช้ใน Firebase Authentication ด้วยอีเมลแฝง
	// --- ส่วนที่แก้ไข ---
	// กำหนด UID เองโดยใช้เบอร์โทร
	params := (&auth.UserToCreate{}).
		UID(newUser.Phone). // <-- กำหนด UID เองตรงนี้
		Email(syntheticEmail).
		Password(newUser.Password).
		DisplayName(newUser.Name)

	userRecord, err := h.AuthClient.CreateUser(c.Request.Context(), params)
	if err != nil {
		// ตรวจสอบ Error ว่าเกิดจากอีเมลซ้ำหรือไม่
		if auth.IsEmailAlreadyExists(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "This phone number is already registered."})
			return
		}
		log.Printf("Error creating user: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// 2. บันทึกข้อมูลลงใน Firestore (ที่สำคัญคือบันทึกเบอร์โทรจริงๆ ไว้)
	userData := map[string]interface{}{
		"name":  newUser.Name,
		"phone": newUser.Phone, // <-- บันทึกเบอร์โทรจริง
		"role":  newUser.Role,
	}
	_, err = h.FirestoreClient.Collection("users").Doc(userRecord.UID).Set(c.Request.Context(), userData)
	if err != nil {
		log.Printf("Error saving user to Firestore: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save user data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User registered successfully", "uid": userRecord.UID})
}
