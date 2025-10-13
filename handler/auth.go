package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"api-flash-dash/model"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
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
		DisplayName(coreData.Name).
		PhotoURL(coreData.ImageProfile)

	userRecord, err := h.AuthClient.CreateUser(c.Request.Context(), params)
	if err != nil {
		return nil, err
	}

	// 2. บันทึกข้อมูลพื้นฐานลงใน Collection "users"
	userData := map[string]interface{}{
		"name":          coreData.Name,
		"phone":         coreData.Phone,
		"role":          role,
		"image_profile": coreData.ImageProfile,
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

func (h *AuthHandler) RegisterRiderHandler(c *gin.Context) {
	var payload model.RegisterRiderPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

//-----------------------------------------------------------------------------------------------------------------------------------------//

// LoginRequest คือ struct สำหรับรับข้อมูลตอนล็อกอิน
type LoginRequest struct {
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginHandler สำหรับเข้าสู่ระบบ และดึงข้อมูลตาม Role
func (h *AuthHandler) LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	syntheticEmail := req.Phone + "@flashdash.app"

	// 1. ยิง API ไปยัง Firebase Auth REST API เพื่อ Sign-in
	// !!! สำคัญ: ควรเก็บ FIREBASE_WEB_API_KEY ไว้ใน Environment Variable !!!
	apiKey := os.Getenv("FIREBASE_WEB_API_KEY")
	restApiURL := "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=" + apiKey

	requestBody, _ := json.Marshal(map[string]interface{}{
		"email":             syntheticEmail,
		"password":          req.Password,
		"returnSecureToken": true,
	})

	resp, err := http.Post(restApiURL, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		log.Printf("Error calling Firebase REST API: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Firebase"})
		return
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Firebase auth failed with status: %d, body: %s", resp.StatusCode, string(body))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid phone number or password"})
		return
	}

	var firebaseResp map[string]interface{}
	json.Unmarshal(body, &firebaseResp)

	idToken := firebaseResp["idToken"].(string)
	uid := firebaseResp["localId"].(string)

	// 2. ดึงข้อมูลพื้นฐานจาก Firestore Collection "users"
	userDoc, err := h.FirestoreClient.Collection("users").Doc(uid).Get(c.Request.Context())
	if err != nil {
		log.Printf("Error getting user from Firestore: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user data"})
		return
	}
	userProfile := userDoc.Data()

	// 3. ตรวจสอบ Role และดึงข้อมูลเพิ่มเติม
	role := userProfile["role"].(string)
	var roleSpecificData interface{} // เตรียมตัวแปรไว้เก็บข้อมูลเฉพาะทาง

	if role == "customer" {
		// ดึงข้อมูลที่อยู่ทั้งหมดจาก sub-collection "addresses"
		var addresses []map[string]interface{}
		iter := h.FirestoreClient.Collection("users").Doc(uid).Collection("addresses").Documents(c.Request.Context())
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("Failed to iterate addresses: %v", err)
				break // หรือจัดการ error ตามความเหมาะสม
			}
			addressData := doc.Data()
			addressData["id"] = doc.Ref.ID // เพิ่ม document ID เข้าไปในข้อมูลด้วย
			addresses = append(addresses, addressData)
		}
		roleSpecificData = addresses

	} else if role == "rider" {
		// ดึงข้อมูล Rider จาก collection "riders"
		riderDoc, err := h.FirestoreClient.Collection("riders").Doc(uid).Get(c.Request.Context())
		if err == nil { // ตรวจสอบว่ามีข้อมูลจริง
			roleSpecificData = riderDoc.Data()
		}
	}

	// 4. รวบรวมข้อมูลทั้งหมดเพื่อส่งกลับ
	c.JSON(http.StatusOK, gin.H{
		"message":          "Login successful",
		"idToken":          idToken,
		"userProfile":      userProfile,
		"roleSpecificData": roleSpecificData, // ข้อมูลจะเปลี่ยนไปตาม Role
	})
}

//-----------------------------------------------------------------------------------------------------------------------------------------//

// UpdateUserProfile คือ Handler สำหรับอัปเดตข้อมูลผู้ใช้
func (h *AuthHandler) UpdateUserProfile(c *gin.Context) {
	// 1. ดึง UID จาก Context
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User UID not found in context"})
		return
	}
	uidStr := uid.(string)

	// 2. รับข้อมูล JSON ที่ส่งมาจากแอป
	var payload model.UpdateProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. เตรียมข้อมูลที่จะอัปเดต
	authParams := &auth.UserToUpdate{}
	var firestoreUpdates []firestore.Update

	if payload.Name != nil {
		authParams.DisplayName(*payload.Name)
		firestoreUpdates = append(firestoreUpdates, firestore.Update{Path: "name", Value: *payload.Name})
	}
	if payload.Password != nil && *payload.Password != "" {
		if len(*payload.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
			return
		}
		authParams.Password(*payload.Password)
	}
	if payload.ImageProfile != nil {
		authParams.PhotoURL(*payload.ImageProfile)
		firestoreUpdates = append(firestoreUpdates, firestore.Update{Path: "image_profile", Value: *payload.ImageProfile})
	}

	// 4. สั่งอัปเดตข้อมูลใน Firebase Authentication
	if _, err := h.AuthClient.UpdateUser(context.Background(), uidStr, authParams); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase Auth user: " + err.Error()})
		return
	}

	// 5. สั่งอัปเดตข้อมูลใน Firestore (ถ้ามี)
	if len(firestoreUpdates) > 0 {
		if _, err := h.FirestoreClient.Collection("users").Doc(uidStr).Update(context.Background(), firestoreUpdates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firestore user: " + err.Error()})
			return
		}
	}

	// **** 6. จุดแก้ไขสำคัญ: ดึงข้อมูลล่าสุดทั้งหมดเพื่อส่งกลับไป ****
	updatedData, err := h.getUserDataByUID(uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated user data: " + err.Error()})
		return
	}

	// ดึง ID Token เดิมจาก Header เพื่อส่งกลับไปให้แอปใช้ต่อ
	authHeader := c.GetHeader("Authorization")
	idToken := strings.Replace(authHeader, "Bearer ", "", 1)

	// 7. ส่งข้อความและข้อมูลที่อัปเดตแล้วกลับไปในโครงสร้างที่สมบูรณ์
	c.JSON(http.StatusOK, gin.H{
		"message":          "อัปเดตโปรไฟล์สำเร็จ",
		"idToken":          idToken,                         // <-- เพิ่ม idToken เข้าไป
		"userProfile":      updatedData["userProfile"],      // <-- แยก userProfile ออกมา
		"roleSpecificData": updatedData["roleSpecificData"], // <-- แยก roleSpecificData ออกมา
	})
}

// --- ฟังก์ชันเสริม (Helper Function) ---
// getUserDataByUID ดึงข้อมูลผู้ใช้ทั้งหมดจาก Firestore ตาม UID
func (h *AuthHandler) getUserDataByUID(uid string) (map[string]interface{}, error) {
	userDoc, err := h.FirestoreClient.Collection("users").Doc(uid).Get(context.Background())
	if err != nil {
		log.Printf("Error getting user from Firestore: %v\n", err)
		return nil, err
	}
	userProfile := userDoc.Data()

	role := userProfile["role"].(string)
	var roleSpecificData interface{}

	if role == "customer" {
		var addresses []map[string]interface{}
		iter := h.FirestoreClient.Collection("users").Doc(uid).Collection("addresses").Documents(context.Background())
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, err
			}
			addressData := doc.Data()
			addressData["id"] = doc.Ref.ID
			addresses = append(addresses, addressData)
		}
		roleSpecificData = addresses
	} else if role == "rider" {
		riderDoc, err := h.FirestoreClient.Collection("riders").Doc(uid).Get(context.Background())
		if err == nil {
			roleSpecificData = riderDoc.Data()
		}
	}

	// สร้างข้อมูลที่จะส่งกลับให้มีโครงสร้างเหมือน LoginResponse
	fullResponse := map[string]interface{}{
		"userProfile":      userProfile,
		"roleSpecificData": roleSpecificData,
	}

	return fullResponse, nil
}

//-----------------------------------------------------------------------------------------------------------------------------------------//

// --- ฟังก์ชันใหม่สำหรับเพิ่มที่อยู่ ---
// AddUserAddress คือ Handler สำหรับเพิ่มที่อยู่ใหม่ใน sub-collection ของผู้ใช้
func (h *AuthHandler) AddUserAddress(c *gin.Context) {
	// 1. ดึง UID จาก Context ที่ Middleware ตั้งค่าไว้
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User UID not found in context"})
		return
	}
	uidStr := uid.(string)

	// 2. รับข้อมูล JSON ของที่อยู่ใหม่
	var payload model.AddressPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. เพิ่มข้อมูลลงใน sub-collection 'addresses' ของผู้ใช้คนนั้น
	// Firestore จะสร้าง Document ID ให้โดยอัตโนมัติ
	_, _, err := h.FirestoreClient.Collection("users").Doc(uidStr).Collection("addresses").Add(context.Background(), payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add new address: " + err.Error()})
		return
	}

	// 4. (แนะนำ) ดึงรายการที่อยู่ทั้งหมดล่าสุดกลับไปให้แอป
	allAddresses, err := h.getAllUserAddresses(uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated address list"})
		return
	}

	// 5. ส่งข้อความและรายการที่อยู่ล่าสุดกลับไป
	c.JSON(http.StatusCreated, gin.H{
		"message":   "เพิ่มที่อยู่สำเร็จ",
		"addresses": allAddresses,
	})
}

// --- ฟังก์ชันใหม่สำหรับอัปเดตที่อยู่ ---
// UpdateUserAddress คือ Handler สำหรับอัปเดตที่อยู่ที่มีอยู่แล้ว
func (h *AuthHandler) UpdateUserAddress(c *gin.Context) {
	// 1. ดึง UID จาก Context
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User UID not found in context"})
		return
	}
	uidStr := uid.(string)

	// 2. ดึง Address ID จาก URL parameter (เช่น /api/user/addresses/xyz123)
	addressId := c.Param("addressId")
	if addressId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Address ID is required"})
		return
	}

	// 3. รับข้อมูล JSON ของที่อยู่ที่จะอัปเดต
	var payload model.AddressPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 4. อัปเดตข้อมูลใน Document ของที่อยู่นั้นๆ (ใช้ Set เพื่อเขียนทับทั้งหมด)
	_, err := h.FirestoreClient.Collection("users").Doc(uidStr).Collection("addresses").Doc(addressId).Set(context.Background(), payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update address: " + err.Error()})
		return
	}

	// 5. (แนะนำ) ดึงรายการที่อยู่ทั้งหมดล่าสุดกลับไปให้แอป
	allAddresses, err := h.getAllUserAddresses(uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated address list"})
		return
	}

	// 6. ส่งข้อความและรายการที่อยู่ล่าสุดกลับไป
	c.JSON(http.StatusOK, gin.H{
		"message":   "อัปเดตที่อยู่สำเร็จ",
		"addresses": allAddresses,
	})
}

// --- ฟังก์ชันเสริม (Helper Function) ---
// getAllUserAddresses ดึงที่อยู่ทั้งหมดของผู้ใช้คนนั้นๆ
func (h *AuthHandler) getAllUserAddresses(uid string) ([]map[string]interface{}, error) {
	var addresses []map[string]interface{}
	iter := h.FirestoreClient.Collection("users").Doc(uid).Collection("addresses").Documents(context.Background())
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		data := doc.Data()

		coords, _ := data["coordinates"].(map[string]interface{})

		address := map[string]interface{}{
			"id":     doc.Ref.ID, // ✅ เพิ่ม id กลับไปด้วย
			"detail": data["detail"],
			"coordinates": map[string]interface{}{
				"latitude":  coords["latitude"],
				"longitude": coords["longitude"],
			},
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

// -----------------------------------------------------------------------------------------------------------------------------------------//
// **** เพิ่มฟังก์ชันใหม่สำหรับค้นหาผู้ใช้ ****
// FindUserByPhone ค้นหาผู้ใช้ด้วยเบอร์โทรศัพท์และคืนค่าชื่อพร้อมที่อยู่ทั้งหมด
func (h *AuthHandler) FindUserByPhone(c *gin.Context) {
	phone := c.Query("phone")
	if phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phone number query parameter is required"})
		return
	}

	query := h.FirestoreClient.Collection("users").Where("phone", "==", phone).Limit(1)
	iter := query.Documents(context.Background())
	doc, err := iter.Next()
	if err == iterator.Done {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบผู้ใช้"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query user data"})
		return
	}

	var userProfile model.UserProfile
	if err := doc.DataTo(&userProfile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user profile"})
		return
	}
	uid := doc.Ref.ID

	// --- 1. จุดแก้ไขหลัก ---
	// สร้าง slice สำหรับเก็บข้อมูลประเภท []model.Address โดยตรง
	var addresses []model.Address

	// 2. ดึงข้อมูลที่อยู่ทั้งหมดจาก sub-collection
	addrIter := h.FirestoreClient.Collection("users").Doc(uid).Collection("addresses").Documents(context.Background())
	for {
		addrDoc, err := addrIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user addresses"})
			return
		}

		// 3. แปลงข้อมูล Firestore แต่ละอันให้เป็น struct model.Address
		var address model.Address
		if err := addrDoc.DataTo(&address); err != nil {
			log.Printf("Could not convert address data: %v", err)
			continue // ข้ามที่อยู่ที่มีปัญหาไป
		}
		address.ID = addrDoc.Ref.ID // เพิ่ม ID เข้าไปใน struct ด้วย

		// 4. เพิ่ม struct ที่แปลงแล้วเข้าไปใน slice
		addresses = append(addresses, address)
	}
	// --- จบส่วนแก้ไข ---

	// 5. สร้างข้อมูลเพื่อส่งกลับ (ตอนนี้ชนิดข้อมูลถูกต้องแล้ว)
	response := model.FindUserResponse{
		Name:         userProfile.Name,
		Phone:        userProfile.Phone,        // ++ เพิ่มเข้ามา
		ImageProfile: userProfile.ImageProfile, // ++ เพิ่มเข้ามา
		Role:         userProfile.Role,         // ++ เพิ่มเข้ามา
		Addresses:    addresses,
	}

	c.JSON(http.StatusOK, response)
}

// CreateDeliveryHandler จัดการการสร้างเอกสารการจัดส่งใหม่
func (h *AuthHandler) CreateDeliveryHandler(c *gin.Context) {
	// 1. ดึง UID ของผู้ส่ง (Sender) จาก Context
	senderUID, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sender UID not found"})
		return
	}
	senderUIDStr := senderUID.(string)

	// 2. รับข้อมูล JSON จากแอป
	var payload model.CreateDeliveryPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. ดึงข้อมูลที่อยู่เต็มๆ ของผู้ส่งและผู้รับจาก Firestore
	// (เพื่อเก็บข้อมูลทั้งหมดไว้ในเอกสาร delivery ป้องกันปัญหาถ้า user ลบที่อยู่ทิ้งในอนาคต)
	senderAddrDoc, err := h.FirestoreClient.Collection("users").Doc(senderUIDStr).Collection("addresses").Doc(payload.SenderAddressID).Get(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not retrieve sender address"})
		return
	}

	receiverAddrDoc, err := h.FirestoreClient.Collection("users").Doc(payload.ReceiverPhone).Collection("addresses").Doc(payload.ReceiverAddressID).Get(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not retrieve receiver address"})
		return
	}

	// 4. สร้างเอกสารใหม่ใน Collection 'deliveries'
	deliveryData := map[string]interface{}{
		"senderUID":       senderUIDStr,
		"senderAddress":   senderAddrDoc.Data(),
		"receiverUID":     payload.ReceiverPhone,
		"receiverAddress": receiverAddrDoc.Data(),
		"itemDescription": payload.ItemDescription,
		"itemImage":       payload.ItemImageFilename,
		"riderNoteImage":  payload.RiderNoteImageFilename,
		"status":          "pending",  // สถานะเริ่มต้น
		"createdAt":       time.Now(), // เวลาที่สร้าง
		"riderUID":        nil,        // ยังไม่มีไรเดอร์รับงาน
	}

	_, _, err = h.FirestoreClient.Collection("deliveries").Add(context.Background(), deliveryData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create delivery record: " + err.Error()})
		return
	}

	// 5. ส่งข้อความกลับไปหาแอป
	c.JSON(http.StatusCreated, gin.H{"message": "สร้างการจัดส่งสำเร็จ!"})
}

// GetUserDeliveries ดึงรายการจัดส่งที่ผู้ใช้เป็น "ผู้ส่ง" และ "ผู้รับ"
func (h *AuthHandler) GetUserDeliveries(c *gin.Context) {
	uid, _ := c.Get("uid")
	uidStr := uid.(string)

	// 1. ค้นหารายการที่ผู้ใช้เป็น "ผู้ส่ง"
	sentDeliveries, err := h.queryDeliveries("senderUID", uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get sent deliveries"})
		return
	}

	// 2. ค้นหารายการที่ผู้ใช้เป็น "ผู้รับ"
	receivedDeliveries, err := h.queryDeliveries("receiverUID", uidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get received deliveries"})
		return
	}

	// 3. ส่งข้อมูลทั้งสองรายการกลับไป
	c.JSON(http.StatusOK, gin.H{
		"sentDeliveries":     sentDeliveries,
		"receivedDeliveries": receivedDeliveries,
	})
}

// --- ฟังก์ชันเสริม (Helper Function) ที่แก้ไขแล้ว ---
// queryDeliveries คือฟังก์ชันที่ใช้ในการค้นหาข้อมูลใน collection 'deliveries'
func (h *AuthHandler) queryDeliveries(field, uid string) ([]model.Delivery, error) {
	var deliveries []model.Delivery
	ctx := context.Background()

	iter := h.FirestoreClient.Collection("deliveries").Where(field, "==", uid).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Failed to iterate deliveries: %v", err)
			return nil, err
		}

		var delivery model.Delivery
		if err := doc.DataTo(&delivery); err != nil {
			log.Printf("Failed to convert delivery data: %v", err)
			continue // ข้ามเอกสารที่มีปัญหา
		}
		delivery.ID = doc.Ref.ID

		// --- จุดแก้ไข: ดึงชื่อและรูปโปรไฟล์ของผู้ส่งและผู้รับ ---
		senderProfile, _ := h.FirestoreClient.Collection("users").Doc(delivery.SenderUID).Get(ctx)
		if senderProfile != nil {
			senderData := senderProfile.Data()
			if name, ok := senderData["name"].(string); ok {
				delivery.SenderName = name
			}
			// ดึงรูปโปรไฟล์ผู้ส่ง
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
			// ดึงรูปโปรไฟล์ผู้รับ
			if img, ok := receiverData["image_profile"].(string); ok {
				delivery.ReceiverImageProfile = img
			}
		}
		// --- จบส่วนแก้ไข ---

		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

// -----------------------------------------------------------------------------------------------------------------------------------------//
