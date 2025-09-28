package model

// Coordinates ใช้สำหรับเก็บข้อมูลพิกัด GPS
type Coordinates struct {
	Latitude  float64 `json:"latitude" firestore:"latitude" binding:"required"`
	Longitude float64 `json:"longitude" firestore:"longitude" binding:"required"`
}

// AddressPayload คือข้อมูลที่แอปจะส่งมาเมื่อ "สร้าง" หรือ "อัปเดต" ที่อยู่
// จะไม่มีฟิลด์ ID เพราะเราไม่ได้ส่ง ID มากับข้อมูลส่วนนี้
type AddressPayload struct {
	Detail      string      `json:"detail" firestore:"detail" binding:"required"`
	Coordinates Coordinates `json:"coordinates" firestore:"coordinates" binding:"required"`
}

// Address คือโครงสร้างข้อมูลสำหรับที่อยู่ 1 แห่งแบบสมบูรณ์
// ใช้สำหรับ "ส่งข้อมูลกลับ" ไปให้แอป (เช่น ตอน Login หรือหลังอัปเดต)
type Address struct {
	// ID จะถูกดึงมาจาก Document ID ของ Firestore
	ID          string      `json:"id" firestore:"-"` // firestore:"-" บอกให้ Firestore ไม่ต้องสนใจฟิลด์นี้
	Detail      string      `json:"detail" firestore:"detail"`
	Coordinates Coordinates `json:"coordinates" firestore:"coordinates"`
}
