package model

import "time"

// CreateDeliveryPayload คือข้อมูลทั้งหมดที่แอปต้องส่งมาเพื่อสร้างการจัดส่งใหม่
// ไม่จำเป็นต้องมี SenderPhone หรือ Status เพราะเซิร์ฟเวอร์จะจัดการเอง
type CreateDeliveryPayload struct {
	ReceiverPhone          string `json:"receiverPhone" binding:"required"`
	SenderAddressID        string `json:"senderAddressId" binding:"required"`
	ReceiverAddressID      string `json:"receiverAddressId" binding:"required"`
	ItemDescription        string `json:"itemDescription" binding:"required"`
	ItemImageFilename      string `json:"itemImageFilename" binding:"required"`
	RiderNoteImageFilename string `json:"riderNoteImageFilename"` // อาจจะไม่มีก็ได้ (Optional)
}

// Delivery คือโครงสร้างข้อมูลสำหรับการจัดส่ง 1 รายการ
// ที่จะถูกดึงมาจาก Firestore และส่งกลับไปให้แอป
type Delivery struct {
	ID              string    `json:"id" firestore:"-"` // ID ของ Document
	SenderUID       string    `json:"senderUID" firestore:"senderUID"`
	ReceiverUID     string    `json:"receiverUID" firestore:"receiverUID"`
	SenderAddress   Address   `json:"senderAddress" firestore:"senderAddress"`
	ReceiverAddress Address   `json:"receiverAddress" firestore:"receiverAddress"`
	ItemDescription string    `json:"itemDescription" firestore:"itemDescription"`
	ItemImage       string    `json:"itemImage" firestore:"itemImage"`
	RiderNoteImage  string    `json:"riderNoteImage" firestore:"riderNoteImage"`
	Status          string    `json:"status" firestore:"status"`
	CreatedAt       time.Time `json:"createdAt" firestore:"createdAt"`
	RiderUID        *string   `json:"riderUID,omitempty" firestore:"riderUID"` // อาจเป็น nil
	SenderName      string    `json:"senderName,omitempty"`                    // จะถูกเติมค่าทีหลัง
	ReceiverName    string    `json:"receiverName,omitempty"`                  // จะถูกเติมค่าทีหลัง
}
