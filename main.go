package main

import (
	"log"

	"api-flash-dash/database" // <-- import database
	"api-flash-dash/handler"
	"api-flash-dash/router"
)

func main() {
	// 1. เรียกใช้ฟังก์ชันเชื่อมต่อฐานข้อมูลจากแพ็กเกจ database
	firestoreClient, authClient, err := database.InitFirebase()
	if err != nil {
		log.Fatalf("Could not initialize database: %v", err)
	}
	defer firestoreClient.Close() // defer ยังคงอยู่ที่นี่
	log.Println("Successfully connected to Firebase services!")

	// 2. สร้าง Handler (เหมือนเดิม)
	authHandler := &handler.AuthHandler{
		FirestoreClient: firestoreClient,
		AuthClient:      authClient,
	}

	// 3. เรียกใช้ฟังก์ชัน SetupRouter (เหมือนเดิม)
	router := router.SetupRouter(authHandler)

	// 4. รันเซิร์ฟเวอร์ (เหมือนเดิม)
	log.Println("Server is running on port 8080")
	router.Run(":8080")
}
