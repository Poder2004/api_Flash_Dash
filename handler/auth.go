package handler

import (
	"log"
	"net/http"

	"api-flash-dash/model"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	FirestoreClient *firestore.Client
	AuthClient      *auth.Client
}

// registerUserCore เป็นฟังก์ชันกลางสำหรับสร้างผู้ใช้ใน Auth และบันทึกข้อมูลพื้นฐานลง Firestore
func (h *AuthHandler) registerUserCore(c *gin.Context, coreData model.UserCore, role string) (*auth.UserRecord, error) {
	syntheticEmail := coreData.Phone + "@flashdash.app"

	// 1. สร้างผู้ใช้ใน Firebase Authentication
	params := (&auth.UserToCreate{}).
		UID(coreData.Phone). // ใช้เบอร์โทรเป็น UID
		Email(syntheticEmail).
		Password(coreData.Password).
		DisplayName(coreData.Name)

	userRecord, err := h.AuthClient.CreateUser(c.Request.Context(), params)
	if err != nil {
		return nil, err
	}

	// 2. บันทึกข้อมูลพื้นฐานลงใน Collection "users"
	userData := map[string]interface{}{
		"name":  coreData.Name,
		"phone": coreData.Phone,
		"role":  role,
	}
	_, err = h.FirestoreClient.Collection("users").Doc(userRecord.UID).Set(c.Request.Context(), userData)
	if err != nil {
		// Optional: ควรมี Logic ลบผู้ใช้ใน Auth ถ้าบันทึก Firestore ไม่สำเร็จ
		return nil, err
	}

	return userRecord, nil
}

// RegisterCustomerHandler สำหรับสมัครสมาชิกเป็น Customer
func (h *AuthHandler) RegisterCustomerHandler(c *gin.Context) {
	var payload model.RegisterCustomerPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// เรียกฟังก์ชันกลางเพื่อสร้างผู้ใช้
	userRecord, err := h.registerUserCore(c, payload.UserCore, "customer")
	if err != nil {
		if auth.IsEmailAlreadyExists(err) || auth.IsUIDAlreadyExists(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "This phone number is already registered."})
			return
		}
		log.Printf("Error creating user core: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// บันทึกข้อมูลที่อยู่ลงใน Sub-collection
	_, _, err = h.FirestoreClient.Collection("users").Doc(userRecord.UID).Collection("addresses").Add(c.Request.Context(), payload.Address)
	if err != nil {
		log.Printf("Error saving address: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save address data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Customer registered successfully", "uid": userRecord.UID})
}

// RegisterRiderHandler สำหรับสมัครสมาชิกเป็น Rider
func (h *AuthHandler) RegisterRiderHandler(c *gin.Context) {
	var payload model.RegisterRiderPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// เรียกฟังก์ชันกลางเพื่อสร้างผู้ใช้
	userRecord, err := h.registerUserCore(c, payload.UserCore, "rider")
	if err != nil {
		if auth.IsEmailAlreadyExists(err) || auth.IsUIDAlreadyExists(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "This phone number is already registered."})
			return
		}
		log.Printf("Error creating user core: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// บันทึกข้อมูล Rider ลงใน Collection "riders"
	_, err = h.FirestoreClient.Collection("riders").Doc(userRecord.UID).Set(c.Request.Context(), payload.Rider)
	if err != nil {
		log.Printf("Error saving rider details: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save rider data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Rider registered successfully", "uid": userRecord.UID})
}
