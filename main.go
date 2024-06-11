package main

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"strings"

	s3 "bam/aws"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
)

func main() {

	// Set up S3 client
	endpoint := os.Getenv("ENDPOINT") // Use the endpoint specific to your region
	accessKeyID := os.Getenv("ACCESS_KEY_ID") 
	secretAccessKey := os.Getenv("SECRET_ACCESS_KEY")
	useSSL := true
	bucketName := os.Getenv("BUCKER_NAME")

	// Create S3 client instance
	s3Client, err := s3.NewS3Client(endpoint, accessKeyID, secretAccessKey, bucketName, useSSL)
	if err != nil {
		log.Fatalln(err)
	}

	app := fiber.New()

	// CRUD bucket function
	app.Post("/create-bucket", func(c *fiber.Ctx) error {
		// Create a bucket if it doesn't exist
		exists, err := s3Client.BucketExists(bucketName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		if !exists {
			err := s3Client.CreateBucket(bucketName)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
			}
			return c.SendString("Bucket created")
		}
		return c.SendString("Bucket already exists")
	})

	// Upload file function
	app.Post("/upload", func(c *fiber.Ctx) error {
		file, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}

		uploadResponse, err := s3Client.UploadFileFromStream(file, file.Header.Get("Content-Type"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		return c.JSON(uploadResponse)
	})

	// API เพื่อรับรหัสไฟล์และเปิดไฟล์
	app.Get("/view-file", func(c *fiber.Ctx) error {
		// รับลิ้งค์จาก query string
		link := c.Query("link")

		// ตรวจสอบว่าลิ้งค์มีรูปแบบที่ถูกต้องหรือไม่
		// ตัวอย่างเช่น ต้องเริ่มต้นด้วย https://monastic-be.s3.amazonaws.com/
		if !strings.HasPrefix(link, "https://monastic-be.s3.amazonaws.com/") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid file link"})
		}

		// แยกชื่อไฟล์ออกจากลิ้งค์
		objectName := link[len("https://monastic-be.s3.amazonaws.com/"):]

		// ดึงข้อมูลไฟล์จาก MinIO
		response, err := s3Client.Client.GetObject(context.Background(), bucketName, objectName, minio.GetObjectOptions{})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		defer response.Close()

		// ตรวจสอบประเภทของเนื้อหา (Content-Type)
		_, err = response.Stat()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		// อ่านข้อมูลไฟล์
		content, err := ioutil.ReadAll(response)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		// ส่งข้อมูลไฟล์กลับไปยังผู้ใช้
		return c.SendString(string(content))
	})

	// Start the Fiber app
	log.Fatal(app.Listen(":3000"))
}
