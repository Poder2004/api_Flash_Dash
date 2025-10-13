package handler

import (
	"api-flash-dash/model"
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
	latlng "google.golang.org/genproto/googleapis/type/latlng"
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

// GetPendingDeliveries ดึงรายการจัดส่งทั้งหมดที่มีสถานะเป็น "pending" สำหรับ Rider
func (h *AuthHandler) GetPendingDeliveries(c *gin.Context) {
	var deliveries []model.Delivery
	ctx := context.Background()

	// 1. สร้าง Query เพื่อดึงข้อมูล delivery ที่มี status เป็น "pending"
	//    และเรียงลำดับจากเก่าที่สุดไปใหม่ที่สุด (เพื่อให้ Rider รับงานที่ค้างนานที่สุดก่อน)
	iter := h.FirestoreClient.Collection("deliveries").
		Where("status", "==", "pending").
		Documents(ctx)

	// 2. วนลูปเพื่ออ่านข้อมูลแต่ละรายการ (เหมือนโค้ดเดิม)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Failed to iterate pending deliveries: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get pending deliveries"})
			return
		}

		var delivery model.Delivery
		if err := doc.DataTo(&delivery); err != nil {
			log.Printf("Failed to convert delivery data: %v", err)
			continue // ข้ามเอกสารที่มีปัญหาไป
		}
		delivery.ID = doc.Ref.ID

		// 3. ดึงข้อมูลโปรไฟล์ของผู้ส่งและผู้รับ (Enrichment - โค้ดส่วนนี้ยังคงมีประโยชน์)
		// เพื่อให้ Rider เห็นว่าใครเป็นผู้ส่งและผู้รับ
		senderProfile, _ := h.FirestoreClient.Collection("users").Doc(delivery.SenderUID).Get(ctx)
		if senderProfile != nil {
			senderData := senderProfile.Data()
			if name, ok := senderData["name"].(string); ok {
				delivery.SenderName = name
			}
			if img, ok := senderData["image_profile"].(string); ok {
				delivery.SenderImageProfile = img
			}
		}

		receiverProfile, _ := h.FirestoreClient.Collection("users").Doc(delivery.ReceiverUID).Get(ctx)
		if receiverProfile != nil {
			receiverData := receiverProfile.Data()
			if name, ok := receiverData["name"].(string); ok {
				delivery.ReceiverName = name
			}
			if img, ok := receiverData["image_profile"].(string); ok {
				delivery.ReceiverImageProfile = img
			}
		}

		deliveries = append(deliveries, delivery)
	}

	// 4. ส่งข้อมูลทั้งหมดกลับไป
	c.JSON(http.StatusOK, gin.H{
		"pendingDeliveries": deliveries,
	})
}

// +++ โค้ดใหม่: ฟังก์ชันสำหรับ Rider รับงาน +++

// AcceptDelivery คือ Handler สำหรับให้ Rider กดรับงาน
func (h *AuthHandler) AcceptDelivery(c *gin.Context) {
	ctx := context.Background()

	// 1. ดึงข้อมูลที่จำเป็นจาก Request
	// deliveryId จะมาจาก URL path เช่น /deliveries/xyz/accept
	deliveryId := c.Param("deliveryId")
	if deliveryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Delivery ID is required"})
		return
	}

	// riderUID จะมาจาก Middleware หลังจากยืนยันตัวตนแล้ว (สมมติว่าเก็บไว้ใน context ชื่อ "uid")
	riderUID, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: Rider UID not found"})
		return
	}
	riderUIDStr, ok := riderUID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Rider UID is not in a valid format"})
		return
	}

	// 2. อัปเดตข้อมูลใน Firestore โดยใช้ Transaction เพื่อความปลอดภัย
	deliveryRef := h.FirestoreClient.Collection("deliveries").Doc(deliveryId)

	err := h.FirestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(deliveryRef) // อ่านข้อมูลล่าสุดภายใน Transaction
		if err != nil {
			return err // คืนค่า error เพื่อให้ Transaction ล้มเหลว
		}

		var delivery model.Delivery
		if err := doc.DataTo(&delivery); err != nil {
			return err
		}

		// 3. **ตรวจสอบเงื่อนไขสำคัญ:** งานนี้ต้องมีสถานะเป็น "pending" เท่านั้น
		if delivery.Status != "pending" {
			// ถ้าสถานะไม่ใช่ "pending" แสดงว่ามีคนอื่นตัดหน้าไปแล้ว
			// คืนค่า error เพื่อยกเลิก Transaction
			return errors.New("delivery is not pending, it may have already been accepted")
		}

		// 4. ถ้าเงื่อนไขถูกต้อง, ทำการอัปเดตข้อมูล
		return tx.Update(deliveryRef, []firestore.Update{
			{Path: "status", Value: "accepted"},    // เปลี่ยน status เป็น "accepted"
			{Path: "riderUID", Value: riderUIDStr}, // อัปเดต riderUID ของคนที่รับงาน
		})
	})

	// 5. ตรวจสอบผลลัพธ์ของ Transaction
	if err != nil {
		// ตรวจสอบว่าเป็น error ที่เราสร้างขึ้นเองหรือไม่
		if err.Error() == "delivery is not pending, it may have already been accepted" {
			log.Printf("Rider %s failed to accept delivery %s: %v", riderUIDStr, deliveryId, err)
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()}) // 409 Conflict เหมาะสมกับสถานการณ์นี้
		} else {
			log.Printf("Transaction failed for accepting delivery %s: %v", deliveryId, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to accept delivery"})
		}
		return
	}

	// 6. หากสำเร็จ ส่งข้อความกลับไป
	log.Printf("Rider %s successfully accepted delivery %s", riderUIDStr, deliveryId)
	c.JSON(http.StatusOK, gin.H{
		"message":    "Delivery accepted successfully",
		"deliveryId": deliveryId,
		"riderUID":   riderUIDStr,
	})
}

func (h *AuthHandler) UpdateRiderLocation(c *gin.Context) {
	// 1. ดึง riderUID (เบอร์โทร) ที่ได้จากการยืนยันตัวตนผ่าน Middleware
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	riderUID := uid.(string)

	// 2. ผูกข้อมูล JSON ที่ส่งมากับ struct
	var request model.LocationUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. เตรียมข้อมูลที่จะอัปเดตใน Firestore
	// เราจะใช้ GeoPoint ซึ่งเป็นชนิดข้อมูลสำหรับพิกัดของ Firestore โดยเฉพาะ
	locationData := &latlng.LatLng{
		Latitude:  request.Latitude,
		Longitude: request.Longitude,
	}

	// 4. อัปเดตข้อมูลใน collection "riders"
	// โดยใช้ riderUID (เบอร์โทร) เป็น ID ของ document
	// ใช้ Set กับ MergeAll เพื่อสร้าง document ถ้ายังไม่มี หรืออัปเดต field ถ้ามีอยู่แล้ว
	_, err := h.FirestoreClient.Collection("riders").Doc(riderUID).Set(context.Background(), map[string]interface{}{
		"currentLocation": locationData,
		"updatedAt":       firestore.ServerTimestamp, // บันทึกเวลาที่อัปเดตล่าสุด
	}, firestore.MergeAll)

	if err != nil {
		log.Printf("Failed to update rider location for %s: %v", riderUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	// 5. ส่งสถานะสำเร็จกลับไป
	c.JSON(http.StatusOK, gin.H{"message": "Location updated successfully"})
}
