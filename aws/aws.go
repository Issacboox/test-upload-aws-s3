package aws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Client struct {
	Client     *minio.Client
	BucketName string
}

func NewS3Client(endpoint, accessKeyID, secretAccessKey, bucketName string, useSSL bool) (*S3Client, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
		Region: os.Getenv("REGION"),
	})
	if err != nil {
		return nil, err
	}

	exists, errBucketExists := minioClient.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && !exists {
		err = minioClient.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
		log.Printf("Successfully created %s\n", bucketName)
	}

	return &S3Client{
		Client:     minioClient,
		BucketName: bucketName,
	}, nil
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

func (s *S3Client) UploadFileFromStream(file *multipart.FileHeader, contentType string) (UploadResponse, error) {
	fileID, err := generateFileID()
	if err != nil {
		return UploadResponse{Status: http.StatusInternalServerError}, err
	}

	// Generate a new file name using UUID
	newFileName := fileID + "-" + uuid.New().String()

	info, err := file.Open()
	if err != nil {
		return UploadResponse{Status: http.StatusInternalServerError}, err
	}
	defer info.Close()

	size := file.Size
	_, err = s.Client.PutObject(context.Background(), s.BucketName, newFileName, info, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return UploadResponse{Status: http.StatusInternalServerError}, err
	}

	reqParams := make(url.Values)
	presignedURL, err := s.Client.PresignedGetObject(context.Background(), s.BucketName, newFileName, time.Duration(0), reqParams)
	if err != nil {
		return UploadResponse{Status: http.StatusInternalServerError}, err
	}

	return UploadResponse{
		Status: http.StatusOK,
		URL:    presignedURL.String(),
	}, nil
}

type UploadResponse struct {
	Status int    `json:"status"`
	URL    string `json:"url"`
}

func generateFileID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetFileURL retrieves the URL of the file based on the file ID and object name
func (s *S3Client) GetFileURL(fileID string, objectName string) (string, error) {
	reqParams := make(url.Values)
	objectNameWithID := fileID + "-" + objectName // Combine fileID and objectName if needed
	presignedURL, err := s.Client.PresignedGetObject(context.Background(), s.BucketName, objectNameWithID, time.Duration(0), reqParams)
	if err != nil {
		return "", err
	}
	return presignedURL.String(), nil
}
