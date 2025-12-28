package storage

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioProvider struct {
	client *minio.Client
	bucket string
}

func NewMinioProvider(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinioProvider, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	// 自动建桶
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err == nil && !exists {
		client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
	}

	return &MinioProvider{client: client, bucket: bucket}, nil
}

func (m *MinioProvider) Save(filename string, data io.Reader) error {
	// 上传对象 (filename 作为 Key)
	// 为了简单，我们不预先计算大小，使用 -1 (但这会消耗更多内存做缓冲)，建议在上层获取大小传进来
	// 这里为了接口统一，先用 PutObject流式
	// 注意：filename 在 MinIO 里作为 ObjectKey，需要用 "/" 分隔
	objectName := strings.ReplaceAll(filename, "\\", "/")

	_, err := m.client.PutObject(context.Background(), m.bucket, objectName, data, -1, minio.PutObjectOptions{
		ContentType: "application/zip",
	})
	return err
}

func (m *MinioProvider) Get(filename string) (io.ReadCloser, error) {
	objectName := strings.ReplaceAll(filename, "\\", "/")
	return m.client.GetObject(context.Background(), m.bucket, objectName, minio.GetObjectOptions{})
}

func (m *MinioProvider) Delete(filename string) error {
	objectName := strings.ReplaceAll(filename, "\\", "/")
	return m.client.RemoveObject(context.Background(), m.bucket, objectName, minio.RemoveObjectOptions{})
}

func (m *MinioProvider) GetDownloadURL(filename string, masterAddr string) (string, error) {
	objectName := strings.ReplaceAll(filename, "\\", "/")

	// 生成预签名 URL (Worker 直接去 MinIO 下载，不经过 Master)
	// 有效期 24 小时
	expiry := time.Hour * 24
	url, err := m.client.PresignedGetObject(context.Background(), m.bucket, objectName, expiry, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

func (m *MinioProvider) ListFiles() ([]FileInfo, error) {
	var files []FileInfo
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	objectCh := m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{Recursive: true})
	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		files = append(files, FileInfo{
			Name:    object.Key,
			Size:    object.Size,
			ModTime: object.LastModified.Unix(),
		})
	}
	return files, nil
}

func (m *MinioProvider) GetUploadURL(filename string, expire time.Duration) (string, error) {
	objectName := strings.ReplaceAll(filename, "\\", "/")

	// 生成 PUT 请求的预签名 URL
	url, err := m.client.PresignedPutObject(context.Background(), m.bucket, objectName, expire)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}
