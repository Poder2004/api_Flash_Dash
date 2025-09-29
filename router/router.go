package router

import (
	"api-flash-dash/handler" // <-- import handler ของเรา
	"api-flash-dash/middleware"

	"github.com/gin-gonic/gin"
)

// SetupRouter ทำหน้าที่ตั้งค่า Routes ทั้งหมด
// เราต้องรับ AuthHandler เข้ามาเพื่อนำไปใช้งาน
func SetupRouter(authHandler *handler.AuthHandler) *gin.Engine {
	// 1. สร้าง Router ด้วย Gin
	router := gin.Default()

	// 2. จัดกลุ่ม Endpoint สำหรับ Auth
	authRoutes := router.Group("/auth")
	{
		// กำหนดเส้นทางใหม่สำหรับการสมัคร
		authRoutes.POST("/register/customer", authHandler.RegisterCustomerHandler)
		authRoutes.POST("/register/rider", authHandler.RegisterRiderHandler)

		authRoutes.POST("/login", authHandler.LoginHandler)
	}

	private := router.Group("/api")
	private.Use(middleware.AuthMiddleware(authHandler.AuthClient))
	{
		// **** จุดแก้ไข: เปลี่ยน userHandler เป็น authHandler ****
		// เพราะเราส่ง authHandler เข้ามาในฟังก์ชันนี้
		private.PUT("/user/profile", authHandler.UpdateUserProfile)

		// เส้นทางสำหรับจัดการที่อยู่
		// Endpoint: POST /api/user/addresses
		private.POST("/user/addresses", authHandler.AddUserAddress)

		// Endpoint: PUT /api/user/addresses/:addressId
		private.PUT("/user/addresses/:addressId", authHandler.UpdateUserAddress)
		// เส้นทางสำหรับค้นหาผู้ใช้
		// Endpoint: GET /api/users/find?phone=xxxxxxxxxx
		private.GET("/users/find", authHandler.FindUserByPhone)

		// --- เส้นทางสำหรับ Rider ---
		// Endpoint: PUT /api/rider/profile
		// เราจะเรียกใช้ฟังก์ชัน UpdateRiderProfile ที่อยู่ใน AuthHandler
		private.PUT("/rider/profile", authHandler.UpdateRiderProfile)
	}

	return router
}
