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

func (h *AuthHandler) UpdateUserProfile(c *gin.Context) {
	// 1. ดึง ID Token จาก Header เพื่อตรวจสอบสิทธิ์
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
		return
	}
	idToken := strings.Replace(authHeader, "Bearer ", "", 1)

	// 2. ตรวจสอบ Token และดึง UID ของผู้ใช้ออกมา
	token, err := h.AuthClient.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid ID token"})
		return
	}
	uid := token.UID

	// 3. รับข้อมูล JSON ที่ส่งมาจากแอป
	var payload model.UpdateProfilePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 4. เตรียมข้อมูลที่จะอัปเดตใน Firebase Authentication
	authParams := &auth.UserToUpdate{}
	firestoreUpdateData := make(map[string]interface{})

	// ตรวจสอบทีละ field ว่ามีการส่งข้อมูลมาหรือไม่
	if payload.Name != nil && *payload.Name != "" {
		authParams.DisplayName(*payload.Name)
		firestoreUpdateData["name"] = *payload.Name
	}
	if payload.Password != nil && *payload.Password != "" {
		// ควรมีการ validate ความยาวรหัสผ่านเพิ่มเติม
		if len(*payload.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
			return
		}
		authParams.Password(*payload.Password)
	}
	if payload.ImageProfile != nil && *payload.ImageProfile != "" {
		// ประกอบร่าง URL เต็มสำหรับ PhotoURL
		authParams.PhotoURL(*payload.ImageProfile)
		firestoreUpdateData["image_profile"] = *payload.ImageProfile // Firestore เก็บแค่ชื่อไฟล์
	}

	// 5. สั่งอัปเดตข้อมูลใน Firebase Authentication
	_, err = h.AuthClient.UpdateUser(context.Background(), uid, authParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase Auth user: " + err.Error()})
		return
	}

	// 6. สั่งอัปเดตข้อมูลใน Firestore
	// ตรวจสอบว่ามีข้อมูลให้อัปเดตหรือไม่ (ป้องกันการอัปเดตค่าว่าง)
	if len(firestoreUpdateData) > 0 {
		_, err = h.FirestoreClient.Collection("users").Doc(uid).Set(context.Background(), firestoreUpdateData, firestore.MergeAll)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firestore user: " + err.Error()})
			return
		}
	}
	// **** 6. ดึงข้อมูลโปรไฟล์ล่าสุดจาก Firestore ****
	userDoc, err := h.FirestoreClient.Collection("users").Doc(uid).Get(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated user profile: " + err.Error()})
		return
	}
	var updatedProfile model.UserProfile
	userDoc.DataTo(&updatedProfile)

	// **** 7. ส่งข้อความพร้อมกับข้อมูลที่อัปเดตแล้วกลับไปหาแอป ****
	c.JSON(http.StatusOK, gin.H{
		"message":        "อัปเดตโปรไฟล์สำเร็จ",
		"updatedProfile": updatedProfile,
	})
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
func (h *AuthHandler) getAllUserAddresses(uid string) ([]model.Address, error) {
	var addresses []model.Address
	iter := h.FirestoreClient.Collection("users").Doc(uid).Collection("addresses").Documents(context.Background())
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var address model.Address
		doc.DataTo(&address)
		address.ID = doc.Ref.ID // เพิ่ม ID ของ Document เข้าไปใน struct
		addresses = append(addresses, address)
	}
	return addresses, nil
}

//-----------------------------------------------------------------------------------------------------------------------------------------//