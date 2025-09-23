package router

import (
	"api-flash-dash/handler" // <-- import handler ของเรา

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
		// authRoutes.POST("/login", authHandler.LoginHandler)
	}

	// คุณสามารถเพิ่ม Route กลุ่มอื่นๆ ได้ที่นี่ เช่น
	// productRoutes := router.Group("/products")
	// {
	//   productRoutes.GET("/", productHandler.GetAll)
	// }

	// 4. คืนค่า router ที่ตั้งค่าเสร็จแล้วกลับไป
	return router
}