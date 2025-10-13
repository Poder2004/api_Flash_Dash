package model

// UserCore เก็บข้อมูลพื้นฐานที่ทุกคนต้องมีตอนสมัคร
// เราจะเพิ่ม ImageProfile เข้ามาในนี้ด้วย
type UserCore struct {
	Name         string `json:"name" binding:"required"`
	Phone        string `json:"phone" binding:"required"` // จะใช้เป็น UID
	Password     string `json:"password" binding:"required,min=6"`
	ImageProfile string `json:"image_profile" binding:"required"`
}

// RegisterCustomerPayload คือข้อมูลทั้งหมดที่ต้องส่งมาตอนสมัครเป็น Customer
type RegisterCustomerPayload struct {
	UserCore
	Address Address `json:"address" binding:"required"`
}

// RegisterRiderPayload คือข้อมูลทั้งหมดที่ต้องส่งมาตอนสมัครเป็น Rider
type RegisterRiderPayload struct {
	UserCore
	Rider Rider `json:"rider_details" binding:"required"`
}

// UserProfile ใช้สำหรับแสดงผลข้อมูลผู้ใช้ (ไม่มีรหัสผ่าน)
// เหมาะสำหรับดึงข้อมูลจาก Firestore กลับมาแสดงผล
type UserProfile struct {
	Name         string `json:"name" firestore:"name"`
	Phone        string `json:"phone" firestore:"phone"` // นี่คือ UID
	ImageProfile string `json:"image_profile" firestore:"image_profile"`
	Role         string `json:"role" firestore:"role"`
}

type UpdateProfilePayload struct {
	Name         *string `json:"name"`
	Password     *string `json:"password"`
	ImageProfile *string `json:"image_profile"`
}

// FindUserResponse คือโครงสร้างข้อมูลที่จะส่งกลับไปเมื่อค้นหาผู้ใช้เจอ
type FindUserResponse struct {
    Name         string    `json:"name"`
    Phone        string    `json:"phone"`         // ++ เพิ่มเข้ามา
    ImageProfile string    `json:"image_profile"` // ++ เพิ่มเข้ามา
    Role         string    `json:"role"`          // ++ เพิ่มเข้ามา
    Addresses    []Address `json:"addresses"`
}