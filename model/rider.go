package model

type Rider struct {
    ImageVehicle        string `json:"image_vehicle" firestore:"image_vehicle"`
    VehicleRegistration string `json:"vehicle_registration" firestore:"vehicle_registration"`
}


type UpdateRiderProfilePayload struct {
	Name                *string `json:"name"`
	Password            *string `json:"password"`
	ImageProfile        *string `json:"image_profile"`
	ImageVehicle        *string `json:"image_vehicle"`
	VehicleRegistration *string `json:"vehicle_registration"`
}

// LocationUpdateRequest เป็น struct สำหรับรับข้อมูลพิกัดจากแอปไรเดอร์
type LocationUpdateRequest struct {
    Latitude  float64 `json:"latitude" binding:"required"`
    Longitude float64 `json:"longitude" binding:"required"`
}