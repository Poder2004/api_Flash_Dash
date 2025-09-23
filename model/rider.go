package model

type Rider struct {
	ImageVehicle        string `json:"image_vehicle" binding:"required"`
	VehicleRegistration string `json:"vehicle_registration" binding:"required"`
}