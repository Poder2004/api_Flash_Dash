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
		// 3. กำหนดเส้นทางให้ตรงกับ Handler
		authRoutes.POST("/register", authHandler.RegisterHandler)
		// authRoutes.POST("/login", authHandler.LoginHandler) // ถ้ามี login ก็เพิ่มตรงนี้
	}

	// คุณสามารถเพิ่ม Route กลุ่มอื่นๆ ได้ที่นี่ เช่น
	// productRoutes := router.Group("/products")
	// {
	//   productRoutes.GET("/", productHandler.GetAll)
	// }

	// 4. คืนค่า router ที่ตั้งค่าเสร็จแล้วกลับไป
	return router
}