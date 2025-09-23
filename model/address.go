package model

// Coordinates ใช้สำหรับเก็บข้อมูลพิกัด GPS โดยเฉพาะ
// การแยก struct ออกมาช่วยให้โค้ดสะอาดและจัดการง่ายขึ้น
type Coordinates struct {
	Latitude  float64 `json:"latitude" firestore:"latitude" binding:"required"`
	Longitude float64 `json:"longitude" firestore:"longitude" binding:"required"`
}

// Address คือโครงสร้างข้อมูลสำหรับที่อยู่ 1 แห่ง
// ข้อมูลนี้จะถูกเก็บเป็น Document ใน Sub-collection ของแต่ละ User
type Address struct {
	// ID ของ Address จะถูกสร้างโดยอัตโนมัติจาก Firestore
	// เราจึงไม่จำเป็นต้องใส่ ID ใน struct นี้ตอนสร้างข้อมูล

	Detail      string      `json:"detail" firestore:"detail" binding:"required"`
	Coordinates Coordinates `json:"coordinates" firestore:"coordinates"`
}
