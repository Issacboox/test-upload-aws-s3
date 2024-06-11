package main

import (
	"log"
	"net/http"
	"os"

	s3 "bam/aws"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	// Set up S3 client
	endpoint := os.Getenv("ENDPOINT") // Use the endpoint specific to your region
	accessKeyID := os.Getenv("ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("SECRET_ACCESS_KEY")
	useSSL := true
	bucketName := os.Getenv("BUCKET_NAME")

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
		form, err := c.MultipartForm()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("Error parsing multipart form")
		}

		files := form.File["file"] // Assuming the file input field is named "file"
		if len(files) == 0 {
			return c.Status(fiber.StatusBadRequest).SendString("No files uploaded")
		}

		uploadResponses, err := s3Client.UploadMultipleFilesFromStream(files, files[0].Header.Get("Content-Type"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		return c.JSON(uploadResponses) // Return a JSON array of responses
	})

	app.Get("/download/:filename/:token", func(c *fiber.Ctx) error {
		fileName := c.Params("filename")
		token := c.Params("token")

		downloadResponse, err := s3Client.GenerateDownloadURLWithFileNameAndToken(fileName, token)
		if err != nil {
			// Handle error (e.g., return an error response to the user)
			return c.Status(downloadResponse.Status).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(downloadResponse)
	})

	app.Delete("/delete/:filename/:token", func(c *fiber.Ctx) error {
		fileName := c.Params("filename")
		token := c.Params("token")

		// ดึง Secret Token จาก Environment Variable
		secretToken := os.Getenv("SECRET_TOKEN")
		if secretToken == "" {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "missing SECRET_TOKEN"})
		}

		// สร้าง Token ใหม่เพื่อตรวจสอบ
		expectedToken, err := s3.GenerateToken(secretToken, fileName) // Use s3.GenerateToken here
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		// ตรวจสอบ Token
		if token != expectedToken {
			return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "invalid token"})
		}

		// err = s3Client.DeleteFile(fileName) // Use = for assignment
		// if err != nil {
		// 	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		// }
		status, err := s3Client.DeleteFile(fileName) // รับค่า status ออกมาด้วย
		if err != nil {
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}

		return c.SendStatus(status) // ส่ง status กลับไป
	})
	// Start the Fiber app
	log.Fatal(app.Listen(":3000"))
}
