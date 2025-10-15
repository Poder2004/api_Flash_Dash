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
	deliveryId := c.Param("deliveryId")
	if deliveryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Delivery ID is required"})
		return
	}

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
			return err
		}

		var delivery model.Delivery
		if err := doc.DataTo(&delivery); err != nil {
			return err
		}

		// 3. **ตรวจสอบเงื่อนไขสำคัญ:** งานนี้ต้องมีสถานะเป็น "pending" เท่านั้น
		if delivery.Status != "pending" {
			return errors.New("delivery is not pending, it may have already been accepted")
		}

		// +++ 4. เพิ่มการตรวจสอบ RiderUID ต้องเป็น nil เท่านั้น +++
		// เนื่องจาก model ของเรา RiderUID เป็น *string (pointer) มันจึงสามารถเป็น nil ได้
		// ถ้า riderUID มีค่าอยู่แล้ว (แม้ status จะเป็น pending) แสดงว่ามีบางอย่างผิดปกติ
		// หรือเป็นงานที่เคยมีคนรับไปแล้ว เราจะไม่อนุญาตให้รับงานนี้ซ้ำซ้อน
		if delivery.RiderUID != nil {
			return errors.New("delivery has already been assigned, cannot be accepted again")
		}

		// 5. ถ้าเงื่อนไขทั้งหมดถูกต้อง, ทำการอัปเดตข้อมูล
		return tx.Update(deliveryRef, []firestore.Update{
			{Path: "status", Value: "accepted"},   // เปลี่ยน status เป็น "accepted"
			{Path: "riderUID", Value: riderUIDStr}, // อัปเดต riderUID ของคนที่รับงาน
		})
	})

	// 6. ตรวจสอบผลลัพธ์ของ Transaction
	if err != nil {
		// ตรวจสอบว่าเป็น error ที่เราสร้างขึ้นเองหรือไม่
		if err.Error() == "delivery is not pending, it may have already been accepted" {
			log.Printf("Rider %s failed to accept delivery %s: %v", riderUIDStr, deliveryId, err)
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()}) // 409 Conflict เหมาะสมกับสถานการณ์นี้
		
		// +++ 7. เพิ่มการจัดการสำหรับ Error ใหม่ที่เราสร้างขึ้น +++
		} else if err.Error() == "delivery has already been assigned, cannot be accepted again" {
			log.Printf("Rider %s failed to accept delivery %s: %v", riderUIDStr, deliveryId, err)
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()}) // 409 Conflict เช่นกัน

		} else {
			log.Printf("Transaction failed for accepting delivery %s: %v", deliveryId, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to accept delivery"})
		}
		return
	}

	// 8. หากสำเร็จ ส่งข้อความกลับไป
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


// +++ ฟังก์ชันใหม่: ยืนยันการรับสินค้า +++
func (h *AuthHandler) ConfirmPickup(c *gin.Context) {
    ctx := context.Background()

    // 1. ดึง deliveryId จาก URL
    deliveryId := c.Param("deliveryId")
    if deliveryId == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Delivery ID is required"})
        return
    }

    // 2. ดึง riderUID จาก Token (เพื่อให้แน่ใจว่าคนที่กดเป็น Rider ที่รับงานจริงๆ)
    uid, exists := c.Get("uid")
    if !exists {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: UID not found"})
        return
    }
    riderUID := uid.(string)


    // 3. รับข้อมูล JSON payload ที่มี URL รูปภาพ
    var payload struct {
        PickupImageURL string `json:"pickupImageURL" binding:"required,url"`
    }
    if err := c.ShouldBindJSON(&payload); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
        return
    }

    // 4. อัปเดตข้อมูลใน Firestore
    deliveryRef := h.FirestoreClient.Collection("deliveries").Doc(deliveryId)

    // ใช้ Transaction เพื่อความปลอดภัยในการตรวจสอบข้อมูลก่อนอัปเดต
    err := h.FirestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
        doc, err := tx.Get(deliveryRef)
        if err != nil {
            return err
        }

        var delivery model.Delivery
        if err := doc.DataTo(&delivery); err != nil {
            return err
        }

        // ตรวจสอบเงื่อนไข:
        // - สถานะต้องเป็น "accepted" เท่านั้น
        // - RiderUID ที่อยู่ในเอกสารต้องตรงกับ Rider ที่ส่ง request มา
        if delivery.Status != "accepted" {
            return errors.New("delivery status is not 'accepted'")
        }
        if delivery.RiderUID == nil || *delivery.RiderUID != riderUID {
            return errors.New("you are not the assigned rider for this delivery")
        }

        // 5. ถ้าเงื่อนไขถูกต้อง, ทำการอัปเดต
        return tx.Update(deliveryRef, []firestore.Update{
            {Path: "status", Value: "picked_up"}, // <-- เปลี่ยนสถานะเป็น "picked_up"
            {Path: "pickupImage", Value: payload.PickupImageURL}, // <-- เพิ่ม field ใหม่สำหรับเก็บรูป
        })
    })


    if err != nil {
        log.Printf("Failed to confirm pickup for delivery %s by rider %s: %v", deliveryId, riderUID, err)
        // ส่ง HTTP Status ที่เหมาะสมกลับไป
        if strings.Contains(err.Error(), "not the assigned rider") || strings.Contains(err.Error(), "not 'accepted'"){
             c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
        } else {
             c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update delivery status"})
        }
        return
    }


    c.JSON(http.StatusOK, gin.H{
        "message": "Pickup confirmed successfully",
        "deliveryId": deliveryId,
        "newStatus": "picked_up",
    })
}

// +++ ฟังก์ชันใหม่: ยืนยันการส่งสินค้า +++
func (h *AuthHandler) ConfirmDelivery(c *gin.Context) {
	ctx := context.Background()

	deliveryId := c.Param("deliveryId")
	if deliveryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Delivery ID is required"})
		return
	}

	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: UID not found"})
		return
	}
	riderUID := uid.(string)

	// รับ URL ของรูปภาพที่ถ่ายตอนส่งของ
	var payload struct {
		DeliveredImageURL string `json:"deliveredImageURL" binding:"required,url"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	deliveryRef := h.FirestoreClient.Collection("deliveries").Doc(deliveryId)

	err := h.FirestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(deliveryRef)
		if err != nil {
			return err
		}

		var delivery model.Delivery
		if err := doc.DataTo(&delivery); err != nil {
			return err
		}

		// ตรวจสอบเงื่อนไข:
		// - สถานะต้องเป็น "picked_up"
		// - RiderUID ต้องตรงกัน
		if delivery.Status != "picked_up" {
			return errors.New("delivery status is not 'picked_up'")
		}
		if delivery.RiderUID == nil || *delivery.RiderUID != riderUID {
			return errors.New("you are not the assigned rider for this delivery")
		}

		// ทำการอัปเดต
		return tx.Update(deliveryRef, []firestore.Update{
			{Path: "status", Value: "delivered"}, // <-- เปลี่ยนสถานะเป็น "delivered"
			{Path: "deliveredImage", Value: payload.DeliveredImageURL}, // <-- เพิ่ม field ใหม่สำหรับรูปตอนส่ง
		})
	})

	if err != nil {
		log.Printf("Failed to confirm delivery for %s: %v", deliveryId, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update delivery status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Delivery confirmed successfully",
		"newStatus": "delivered",
	})
}



// GetCurrentDelivery ตรวจสอบและดึงข้อมูลการจัดส่งที่ไรเดอร์กำลังทำอยู่
func (h *AuthHandler) GetCurrentDelivery(c *gin.Context) {
	ctx := context.Background()

	// 1. ดึง riderUID จาก Token ที่ Middleware ส่งมาให้
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: UID not found"})
		return
	}
	riderUID := uid.(string)

	var activeDelivery model.Delivery

	// 2. สร้าง Query เพื่อค้นหางานที่ Active อยู่
	// เราจะค้นหางานที่ riderUID ตรงกัน และ status เป็น 'accepted' หรือ 'picked_up'
	// ใช้ Limit(1) เพราะไรเดอร์ควรจะมีงานที่ทำค้างอยู่ได้แค่งานเดียว
	query := h.FirestoreClient.Collection("deliveries").
		Where("riderUID", "==", riderUID).
		Where("status", "in", []string{"accepted", "picked_up"}).
		Limit(1)

	iter := query.Documents(ctx)
	doc, err := iter.Next()

	// 3. ตรวจสอบผลลัพธ์
	if err == iterator.Done {
		// iterator.Done หมายถึง วนลูปจนสุดแล้ว แต่ไม่เจอข้อมูลเลย
		log.Printf("No active delivery found for rider %s", riderUID)
		c.Status(http.StatusNoContent) // 204 No Content: สำเร็จแต่ไม่มีข้อมูลจะส่งกลับ
		return
	}
	if err != nil {
		log.Printf("Failed to query for active delivery for rider %s: %v", riderUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get active delivery"})
		return
	}

	// 4. ถ้าเจอข้อมูล, แปลงข้อมูลและส่งกลับ
	if err := doc.DataTo(&activeDelivery); err != nil {
		log.Printf("Failed to convert active delivery data: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process delivery data"})
		return
	}
	activeDelivery.ID = doc.Ref.ID

	// 5. (Optional but Recommended) ดึงข้อมูลเพิ่มเติมเหมือนตอนดึง Pending
	// เพื่อให้ข้อมูลที่ส่งกลับไปครบถ้วนสมบูรณ์
	senderProfile, _ := h.FirestoreClient.Collection("users").Doc(activeDelivery.SenderUID).Get(ctx)
	if senderProfile != nil {
		senderData := senderProfile.Data()
		if name, ok := senderData["name"].(string); ok {
			activeDelivery.SenderName = name
		}
		if img, ok := senderData["image_profile"].(string); ok {
			activeDelivery.SenderImageProfile = img
		}
	}

	receiverProfile, _ := h.FirestoreClient.Collection("users").Doc(activeDelivery.ReceiverUID).Get(ctx)
	if receiverProfile != nil {
		receiverData := receiverProfile.Data()
		if name, ok := receiverData["name"].(string); ok {
			activeDelivery.ReceiverName = name
		}
		if img, ok := receiverData["image_profile"].(string); ok {
			activeDelivery.ReceiverImageProfile = img
		}
	}

	// 6. ส่งข้อมูลของงานที่ค้างอยู่กลับไป
	c.JSON(http.StatusOK, activeDelivery)
}
