package aws

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Client struct {
	Client       *minio.Client
	BucketName   string
	bucketExists bool // Add cache for bucket existence
	tokenMap     map[string]FileInfo
}

func NewS3Client(endpoint, accessKeyID, secretAccessKey, bucketName string, useSSL bool) (*S3Client, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
		Region: os.Getenv("REGION"),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating MinIO client: %w", err) // Enhanced error message
	}

	s3Client := &S3Client{
		Client:     minioClient,
		BucketName: bucketName,
		tokenMap:   make(map[string]FileInfo),
	}

	exists, err := s3Client.Client.BucketExists(context.Background(), bucketName)
	if err != nil {
		return nil, fmt.Errorf("error checking bucket existence: %w", err) // Enhanced error message
	}

	if !exists {
		err = s3Client.Client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("error creating bucket: %w", err) // Enhanced error message
		}
		log.Printf("Successfully created bucket: %s\n", bucketName)
	}

	s3Client.bucketExists = exists // Update cache

	return s3Client, nil
}

func (s *S3Client) CreateBucket(bucketName string) error {
	err := s.Client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
	if err != nil {
		exists, errBucketExists := s.Client.BucketExists(context.Background(), bucketName)
		if errBucketExists == nil && exists {
			log.Printf("Bucket %s already exists\n", bucketName)
			return nil
		}
		return err
	}
	log.Printf("Successfully created %s\n", bucketName)
	return nil
}

func (s *S3Client) DeleteBucket(bucketName string) error {
	err := s.Client.RemoveBucket(context.Background(), bucketName)
	if err != nil {
		return err
	}
	log.Printf("Successfully deleted %s\n", bucketName)
	return nil
}

func (s *S3Client) BucketExists(bucketName string) (bool, error) {
	exists, err := s.Client.BucketExists(context.Background(), bucketName)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *S3Client) ListBuckets() ([]minio.BucketInfo, error) {
	buckets, err := s.Client.ListBuckets(context.Background())
	if err != nil {
		return nil, err
	}
	return buckets, nil
}

type FileInfo struct {
	FileName  string    `json:"file_name"`
	ExpiredAt time.Time `json:"expired_at"`
}

type ReqLinkResponse struct {
	Status   int    `json:"status"`
	Token    string `json:"token"`
	FileName string `json:"file_name"` // เพิ่ม field FileName
	URL      string `json:"url"`
}

type GenerateURLResponse struct {
	Status int    `json:"status"`
	URL    string `json:"url"`
}

func GenerateToken(secretToken, fileName string) (string, error) {
	// ใช้ HMAC-SHA256 เพื่อสร้าง Token จาก secretToken และ fileName
	h := hmac.New(sha256.New, []byte(secretToken))
	h.Write([]byte(fileName))
	token := hex.EncodeToString(h.Sum(nil))
	return token, nil
}

func (s *S3Client) UploadMultipleFilesFromStream(files []*multipart.FileHeader, contentType string) ([]ReqLinkResponse, error) {
	responses := make([]ReqLinkResponse, 0, len(files)) // Preallocate slice for efficiency
	secretToken := os.Getenv("SECRET_TOKEN")

	if secretToken == "" {
		return nil, errors.New("missing SECRET_TOKEN")
	}

	for _, file := range files {
		fileID, err := generateFileID()
		if err != nil {
			return nil, err // Return error immediately if file ID generation fails
		}

		newFileName := fileID + "-" + uuid.New().String()
		info, err := file.Open()
		if err != nil {
			return nil, err // Return error immediately if file opening fails
		}
		defer info.Close()

		size := file.Size
		_, err = s.Client.PutObject(context.Background(), s.BucketName, newFileName, info, size, minio.PutObjectOptions{ContentType: contentType})
		if err != nil {
			return nil, err // Return error immediately if upload fails
		}

		// สร้าง Token โดยใช้ Secret Token และชื่อไฟล์
		token, err := GenerateToken(secretToken, newFileName)
		if err != nil {
			return nil, err
		}

		// สร้าง Presigned URL
		expirationTime := time.Hour
		presignedURL, err := s.Client.PresignedGetObject(context.Background(), s.BucketName, newFileName, expirationTime, nil)
		if err != nil {
			return nil, err
		}

		// เก็บ Token และข้อมูลไฟล์ (ในหน่วยความจำ, หรือคุณสามารถปรับให้บันทึกลงฐานข้อมูลได้)
		fileInfo := FileInfo{
			FileName:  newFileName,
			ExpiredAt: time.Now().Add(time.Hour * 24 * 7),
		}
		s.tokenMap[token] = fileInfo

		responses = append(responses, ReqLinkResponse{
			Status:   http.StatusOK,
			Token:    token,
			FileName: newFileName,
			URL:      presignedURL.String(),
		})
	}

	return responses, nil
}

func (s *S3Client) GenerateDownloadURLWithFileNameAndToken(fileName, token string) (GenerateURLResponse, error) {
	// ดึง Secret Token จาก Environment Variable
	secretToken := os.Getenv("SECRET_TOKEN")
	if secretToken == "" {
		return GenerateURLResponse{Status: http.StatusInternalServerError}, errors.New("missing SECRET_TOKEN")
	}

	// สร้าง Token ใหม่เพื่อตรวจสอบ
	expectedToken, err := GenerateToken(secretToken, fileName)
	if err != nil {
		return GenerateURLResponse{Status: http.StatusInternalServerError}, err
	}

	// ตรวจสอบ Token
	if token != expectedToken {
		return GenerateURLResponse{Status: http.StatusUnauthorized}, errors.New("invalid token")
	}

	// สร้าง Presigned URL ใหม่
	expirationTime := time.Hour
	presignedURL, err := s.Client.PresignedGetObject(context.Background(), s.BucketName, fileName, expirationTime, nil)
	if err != nil {
		return GenerateURLResponse{Status: http.StatusInternalServerError}, err
	}

	return GenerateURLResponse{
		Status: http.StatusOK,
		URL:    presignedURL.String(),
	}, nil
}

func generateFileID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *S3Client) DeleteFile(fileName string) (int, error) {
	// 1. ลบไฟล์จาก S3 bucket
	err := s.Client.RemoveObject(context.Background(), s.BucketName, fileName, minio.RemoveObjectOptions{})
	if err != nil {
		// ตรวจสอบ Error เฉพาะของ MinIO
		if minioErr, ok := err.(minio.ErrorResponse); ok {
			if minioErr.Code == "NoSuchKey" { // ไฟล์ไม่พบ
				return http.StatusNotFound, fmt.Errorf("file not found: %w", err)
			}
		}
		return http.StatusInternalServerError, fmt.Errorf("failed to delete file from S3: %w", err)
	}

	// 2. ลบ Token ที่เกี่ยวข้องออกจาก tokenMap
	for token, fileInfo := range s.tokenMap {
		if fileInfo.FileName == fileName {
			delete(s.tokenMap, token)
			break // หยุด loop เมื่อเจอ Token ที่ตรงกัน
		}
	}

	return http.StatusOK, nil // ส่งกลับ status 200 OK หากลบไฟล์สำเร็จ
}
