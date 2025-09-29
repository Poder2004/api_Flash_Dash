package handler

import (
	"api-flash-dash/model"
	"context"
	"net/http"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

// +++ ฟังก์ชันใหม่สำหรับอัปเดตโปรไฟล์ Rider +++
func (h *AuthHandler) UpdateRiderProfile(c *gin.Context) {
	ctx := context.Background()

	// 1. ดึงเบอร์โทรศัพท์ (UID) จาก Token ที่ผ่าน Middleware มาแล้ว
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: UID not found in context"})
		return
	}
	phone := uid.(string)

	// 2. รับข้อมูล JSON payload ที่ส่งมาจากแอป
	var payload model.UpdateRiderProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. เตรียมข้อมูลที่จะอัปเดตแยกตาม Collection
	userUpdateData := make(map[string]interface{})
	riderUpdateData := make(map[string]interface{})
	authParams := &auth.UserToUpdate{}

	// --- ตรวจสอบข้อมูลส่วนตัว (สำหรับ Collection 'users') ---
	if payload.Name != nil {
		userUpdateData["name"] = *payload.Name
		authParams.DisplayName(*payload.Name)
	}
	if payload.ImageProfile != nil {
		userUpdateData["image_profile"] = *payload.ImageProfile
		authParams.PhotoURL(*payload.ImageProfile)
	}
	if payload.Password != nil {
		if len(*payload.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
			return
		}
		authParams.Password(*payload.Password)
	}

	// --- ตรวจสอบข้อมูล Rider (สำหรับ Collection 'riders') ---
	if payload.ImageVehicle != nil {
		riderUpdateData["image_vehicle"] = *payload.ImageVehicle
	}
	if payload.VehicleRegistration != nil {
		riderUpdateData["vehicle_registration"] = *payload.VehicleRegistration
	}

	// 4. อัปเดตข้อมูลใน Firebase Authentication (ถ้ามี)
	if payload.Name != nil || payload.ImageProfile != nil || payload.Password != nil {
		_, err := h.AuthClient.UpdateUser(ctx, phone, authParams)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase Auth user: " + err.Error()})
			return
		}
	}

	// 5. อัปเดตข้อมูลใน Firestore Collection 'users' (ถ้ามี)
	if len(userUpdateData) > 0 {
		_, err := h.FirestoreClient.Collection("users").Doc(phone).Set(ctx, userUpdateData, firestore.MergeAll)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update 'users' collection: " + err.Error()})
			return
		}
	}

	// 6. อัปเดตข้อมูลใน Firestore Collection 'riders' (ถ้ามี)
	if len(riderUpdateData) > 0 {
		_, err := h.FirestoreClient.Collection("riders").Doc(phone).Set(ctx, riderUpdateData, firestore.MergeAll)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update 'riders' collection: " + err.Error()})
			return
		}
	}

	// 7. ดึงข้อมูลล่าสุดทั้งหมดเพื่อส่งกลับ
	userDoc, err := h.FirestoreClient.Collection("users").Doc(phone).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated user profile"})
		return
	}
	var updatedProfile model.UserProfile
	userDoc.DataTo(&updatedProfile)

	riderDoc, err := h.FirestoreClient.Collection("riders").Doc(phone).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated rider details"})
		return
	}
	var updatedRiderDetails model.Rider
	riderDoc.DataTo(&updatedRiderDetails)

	// 8. ส่งข้อมูลที่อัปเดตแล้วทั้งหมดกลับไป
	c.JSON(http.StatusOK, gin.H{
		"message": "อัปเดตโปรไฟล์ Rider สำเร็จ",
		"updatedData": gin.H{
			"userProfile":      updatedProfile,
			"roleSpecificData": updatedRiderDetails,
		},
	})
}
