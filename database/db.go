package database

import (
	"context"
	"log" // เพิ่ม log เข้ามาเผื่อ debug
	"os"   // Import 'os' เพื่ออ่าน Environment Variables

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/joho/godotenv" // <-- Import ไลบรารีที่เพิ่งติดตั้ง
	"google.golang.org/api/option"
)

// InitFirebase ทำหน้าที่เชื่อมต่อ Firebase และคืนค่า Clients กลับไป
func InitFirebase() (*firestore.Client, *auth.Client, error) {
	// --- ส่วนที่แก้ไข ---
	// 1. โหลดค่าจากไฟล์ .env เข้าสู่ระบบ
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found, reading from environment variables")
	}

	// 2. อ่านค่า Path ของไฟล์ Key จาก Environment Variable
	credentialsPath := os.Getenv("FIREBASE_CREDENTIALS_PATH")
	if credentialsPath == "" {
		log.Fatalf("FIREBASE_CREDENTIALS_PATH environment variable not set")
	}
	// --- จบส่วนแก้ไข ---

	ctx := context.Background()
	// 3. ใช้ Path ที่อ่านได้จาก .env ในการเชื่อมต่อ
	opt := option.WithCredentialsFile(credentialsPath)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return nil, nil, err
	}

	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, nil, err
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, nil, err
	}

	return firestoreClient, authClient, nil
}