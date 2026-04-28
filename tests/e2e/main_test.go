package e2e

import (
	"context"
	"os"
	"queuebuzz/tests/e2e/setup"
	"testing"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
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
	r2Endpoint string
	toxiClient *toxiproxy.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start MongoDB
	mongoC, err := mongodb.Run(ctx, "mongo:6.0")
	if err != nil {
		panic(err)
	}
	mongoURI, _ = mongoC.ConnectionString(ctx)

	// Start Redis
	redisC, err := redis.Run(ctx, "redis:7-alpine")
	if err != nil {
		panic(err)
	}
	redisURI, _ = redisC.ConnectionString(ctx)

	// Start Minio (S3 compatible)
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
	if err != nil {
		panic(err)
	}
	minioHost, _ := minioC.Host(ctx)
	minioPort, _ := minioC.MappedPort(ctx, "9000")
	r2Endpoint = "http://" + minioHost + ":" + minioPort.Port()

	// Set R2 env vars for the app to pick up
	os.Setenv("R2_ACCESS_KEY_ID", "minioadmin")
	os.Setenv("R2_SECRET_ACCESS_KEY", "minioadmin")
	os.Setenv("R2_ENDPOINT", r2Endpoint)
	os.Setenv("R2_BUCKET_NAME", "queuebuzz-test")
	os.Setenv("R2_PUBLIC_URL", r2Endpoint+"/queuebuzz-test")

	// Create the bucket in Minio
	s3Client := s3.New(s3.Options{
		BaseEndpoint: aws.String(r2Endpoint),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", ""),
		UsePathStyle: true,
	})
	_, _ = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String("queuebuzz-test"),
	})

	// Set public policy for the bucket
	policy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::queuebuzz-test/*"]
			}
		]
	}`
	_, _ = s3Client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String("queuebuzz-test"),
		Policy: aws.String(policy),
	})

	// Start Toxiproxy (optional, but keep it if used)
	// For now, we'll pass nil if not strictly needed or if we haven't set up the container
	toxiClient = nil

	code := m.Run()

	setup.TeardownSharedSuite()
	_ = mongoC.Terminate(ctx)
	_ = redisC.Terminate(ctx)
	_ = minioC.Terminate(ctx)

	os.Exit(code)
}

func Suite(t *testing.T) *setup.TestSuite {
	return setup.NewTestSuite(t, mongoURI, redisURI, toxiClient)
}
