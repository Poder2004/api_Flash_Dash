package handler

import (
	"api-flash-dash/model"
	"context"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

// +++ ฟังก์ชันใหม่สำหรับอัปเดตโปรไฟล์ Rider (ฉบับปรับปรุง) +++
func (h *AuthHandler) UpdateRiderProfile(c *gin.Context) {
	ctx := context.Background()

	// 1. ดึง UID จาก Token
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: UID not found in context"})
		return
	}
	uidStr := uid.(string)

	// 2. รับข้อมูล JSON payload
	var payload model.UpdateRiderProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. เตรียมข้อมูลที่จะอัปเดต
	authParams := &auth.UserToUpdate{}
	var firestoreUpdatesUsers []firestore.Update
	var firestoreUpdatesRiders []firestore.Update

	// --- ข้อมูลสำหรับ Collection 'users' และ Firebase Auth ---
	if payload.Name != nil {
		authParams.DisplayName(*payload.Name)
		firestoreUpdatesUsers = append(firestoreUpdatesUsers, firestore.Update{Path: "name", Value: *payload.Name})
	}
	if payload.ImageProfile != nil {
		authParams.PhotoURL(*payload.ImageProfile)
		firestoreUpdatesUsers = append(firestoreUpdatesUsers, firestore.Update{Path: "image_profile", Value: *payload.ImageProfile})
	}
	if payload.Password != nil && *payload.Password != "" {
		if len(*payload.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
			return
		}
		authParams.Password(*payload.Password)
	}

	// --- ข้อมูลสำหรับ Collection 'riders' ---
	if payload.ImageVehicle != nil {
		firestoreUpdatesRiders = append(firestoreUpdatesRiders, firestore.Update{Path: "image_vehicle", Value: *payload.ImageVehicle})
	}
	if payload.VehicleRegistration != nil {
		firestoreUpdatesRiders = append(firestoreUpdatesRiders, firestore.Update{Path: "vehicle_registration", Value: *payload.VehicleRegistration})
	}

	// 4. อัปเดตข้อมูลใน Firebase Authentication (ถ้ามี)
	// ตรวจสอบว่ามีข้อมูลที่ต้องอัปเดตใน Auth หรือไม่ เพื่อลดการเรียก API ที่ไม่จำเป็น
	if payload.Name != nil || payload.ImageProfile != nil || (payload.Password != nil && *payload.Password != "") {
		if _, err := h.AuthClient.UpdateUser(ctx, uidStr, authParams); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase Auth user: " + err.Error()})
			return
		}
	}

	// 5. [ปรับปรุง] ใช้ BATCH WRITE เพื่ออัปเดต Firestore ทั้ง 2 Collections ในครั้งเดียว
	batch := h.FirestoreClient.Batch()

	// เพิ่มการอัปเดตสำหรับ 'users' collection เข้าไปใน batch
	if len(firestoreUpdatesUsers) > 0 {
		userRef := h.FirestoreClient.Collection("users").Doc(uidStr)
		batch.Update(userRef, firestoreUpdatesUsers)
	}

	// เพิ่มการอัปเดตสำหรับ 'riders' collection เข้าไปใน batch
	if len(firestoreUpdatesRiders) > 0 {
		riderRef := h.FirestoreClient.Collection("riders").Doc(uidStr)
		batch.Update(riderRef, firestoreUpdatesRiders)
	}

	// สั่งทำงาน batch (ถ้ามีอะไรให้อัปเดต)
	if len(firestoreUpdatesUsers) > 0 || len(firestoreUpdatesRiders) > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit Firestore batch update: " + err.Error()})
			return
		}
	}

	// 6. [ปรับปรุง] ดึงข้อมูลล่าสุดทั้งหมดด้วยฟังก์ชันช่วย
	updatedData, err := h.getRiderDataByUID(uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated rider data: " + err.Error()})
		return
	}

	// 7. [ปรับปรุง] ดึง ID Token เดิมจาก Header
	authHeader := c.GetHeader("Authorization")
	idToken := strings.Replace(authHeader, "Bearer ", "", 1)

	// 8. ส่ง Response กลับในโครงสร้างที่สมบูรณ์
	c.JSON(http.StatusOK, gin.H{
		"message":          "อัปเดตโปรไฟล์ Rider สำเร็จ",
		"idToken":          idToken, // <-- เพิ่ม idToken
		"userProfile":      updatedData["userProfile"],
		"roleSpecificData": updatedData["roleSpecificData"],
	})
}

// +++ ฟังก์ชันช่วยสำหรับดึงข้อมูล Rider (ปรับปรุงตามตัวอย่าง) +++
func (h *AuthHandler) getRiderDataByUID(uid string) (map[string]interface{}, error) {
	ctx := context.Background()

	// 1. ดึงข้อมูลจาก 'users' collection
	userDoc, err := h.FirestoreClient.Collection("users").Doc(uid).Get(ctx)
	if err != nil {
		log.Printf("Error getting user from Firestore: %v\n", err)
		return nil, err
	}
	userProfile := userDoc.Data()

	// 2. ดึงข้อมูลจาก 'riders' collection
	riderDoc, err := h.FirestoreClient.Collection("riders").Doc(uid).Get(ctx)
	if err != nil {
		log.Printf("Error getting rider data from Firestore: %v\n", err)
		return nil, err
	}
	riderDetails := riderDoc.Data()

	// 3. สร้างข้อมูลที่จะส่งกลับ
	fullResponse := map[string]interface{}{
		"userProfile":      userProfile,
		"roleSpecificData": riderDetails,
	}

	return fullResponse, nil
}

//yesss


