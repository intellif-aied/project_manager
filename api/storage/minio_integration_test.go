package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/aidashboard/api/config"
)

func TestMinioPutVerifiedIntegration(t *testing.T) {
	endpoint := os.Getenv("AIDA_TEST_MINIO_ENDPOINT")
	accessKey := os.Getenv("AIDA_TEST_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("AIDA_TEST_MINIO_SECRET_KEY")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("AIDA_TEST_MINIO_* is not configured")
	}
	store, err := NewMinioStorage(&config.Config{
		MinioEndpoint: endpoint, MinioAccessKey: accessKey, MinioSecretKey: secretKey,
		MinioBucket: "aidashboard-v2-test", MinioUseSSL: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	objectKey := fmt.Sprintf("verified/%d.jsonl", time.Now().UnixNano())
	content := []byte("{\"event\":1}\n")
	hash := sha256.Sum256(content)
	if err := store.PutVerified(ctx, objectKey, bytes.NewReader(content), int64(len(content)), hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	defer store.Delete(context.Background(), objectKey)
	reader, err := store.Download(ctx, objectKey)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored=%q err=%v", stored, err)
	}

	badObjectKey := objectKey + ".bad"
	wrongHash := sha256.Sum256([]byte("different"))
	if err := store.PutVerified(ctx, badObjectKey, bytes.NewReader(content), int64(len(content)), hex.EncodeToString(wrongHash[:])); err == nil {
		t.Fatal("hash mismatch was accepted")
	}
	badReader, err := store.Download(ctx, badObjectKey)
	if err == nil {
		_, err = io.ReadAll(badReader)
		badReader.Close()
	}
	if err == nil {
		t.Fatal("hash mismatch object was not removed")
	}
}
