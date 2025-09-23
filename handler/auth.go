package handler

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

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
