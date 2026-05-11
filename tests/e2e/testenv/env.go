package testenv

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	mongoURI   string
	redisURI   string
	mailpitURL string
	cleanup    func()
	setupOnce  sync.Once
)

func Setup() (string, string, string) {
	setupOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var terminators []func(context.Context) error

		mongodbContainer, err := mongodb.Run(ctx, "mongo:6.0")
		if err == nil {
			mongoURI, _ = mongodbContainer.ConnectionString(ctx)
			terminators = append(terminators, func(ctx context.Context) error {
				return mongodbContainer.Terminate(ctx)
			})
		}

		redisContainer, err := redis.Run(ctx, "redis:7-alpine")
		if err == nil {
			redisURI, _ = redisContainer.ConnectionString(ctx)
			terminators = append(terminators, func(ctx context.Context) error {
				return redisContainer.Terminate(ctx)
			})
		}

		minioC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "minio/minio",
				ExposedPorts: []string{"9000/tcp"},
				Cmd:          []string{"server", "/data"},
				Env: map[string]string{
					"MINIO_ROOT_USER":     "minioadmin",
					"MINIO_ROOT_PASSWORD": "minioadmin",
				},
				WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000/tcp"),
			},
			Started: true,
		})
		if err == nil {
			terminators = append(terminators, func(ctx context.Context) error {
				return minioC.Terminate(ctx)
			})
			minioHost, _ := minioC.Host(ctx)
			if minioHost == "localhost" {
				minioHost = "127.0.0.1"
			}
			minioPort, _ := minioC.MappedPort(ctx, "9000")
			r2Endpoint := "http://" + minioHost + ":" + minioPort.Port()
			fmt.Printf("DEBUG: R2_ENDPOINT set to: %s\n", r2Endpoint)

			os.Setenv("R2_ACCESS_KEY_ID", "minioadmin")
			os.Setenv("R2_SECRET_ACCESS_KEY", "minioadmin")
			os.Setenv("R2_ENDPOINT", r2Endpoint)
			os.Setenv("R2_BUCKET_NAME", "queuebuzz-test")
			os.Setenv("R2_PUBLIC_URL", r2Endpoint+"/queuebuzz-test")

			// Create bucket
			s3Client := s3.New(s3.Options{
				BaseEndpoint: aws.String(r2Endpoint),
				Region:       "us-east-1",
				Credentials:  credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", ""),
				UsePathStyle: true,
			})
			_, _ = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String("queuebuzz-test"),
			})
		}

		// Start Mailpit
		mailpitC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "axllent/mailpit",
				ExposedPorts: []string{"1025/tcp", "8025/tcp"},
				WaitingFor:   wait.ForHTTP("/").WithPort("8025/tcp"),
			},
			Started: true,
		})
		if err == nil {
			terminators = append(terminators, func(ctx context.Context) error {
				return mailpitC.Terminate(ctx)
			})
			mailHost, _ := mailpitC.Host(ctx)
			mailPort, _ := mailpitC.MappedPort(ctx, "1025")
			mailAPIPort, _ := mailpitC.MappedPort(ctx, "8025")
			mailpitURL = "http://" + mailHost + ":" + mailAPIPort.Port()
			os.Setenv("MAILPIT_SMTP_HOST", mailHost)
			os.Setenv("MAILPIT_SMTP_PORT", mailPort.Port())
			os.Setenv("MAILPIT_API_URL", mailpitURL)
		}

		cleanup = func() {
			for _, t := range terminators {
				_ = t(context.Background())
			}
		}
	})

	// Ensure environment variables are set for the current process even if once.Do was already called
	if mailpitURL != "" {
		os.Setenv("MAILPIT_API_URL", mailpitURL)
	}

	return mongoURI, redisURI, mailpitURL
}

func Teardown() {
	if cleanup != nil {
		cleanup()
	}
}
