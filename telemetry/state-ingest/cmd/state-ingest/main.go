package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mr-tron/base58/base58"
)

type Metadata struct {
	SnapshotTimestamp string `json:"snapshot_timestamp"`
	Command           string `json:"command"`
	DevicePubkey      string `json:"device_pubkey"`
	Kind              string `json:"kind"`
}

type PushRequest struct {
	Metadata Metadata        `json:"metadata"`
	Data     json.RawMessage `json:"data"`
}

type Server struct {
	s3     *s3.Client
	bucket string
	prefix string
}

func sanitizePathComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request, kindOverride string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pubkeyB58 := r.Header.Get("X-DoubleZero-Pubkey")
	sigB58 := r.Header.Get("X-DoubleZero-Signature")
	tsHeader := r.Header.Get("X-DoubleZero-Timestamp")
	if pubkeyB58 == "" || sigB58 == "" || tsHeader == "" {
		http.Error(w, "missing auth headers", http.StatusUnauthorized)
		return
	}

	clientTS, err := time.Parse(time.RFC3339, tsHeader)
	if err != nil {
		http.Error(w, "invalid X-DoubleZero-Timestamp", http.StatusUnauthorized)
		return
	}
	if d := time.Since(clientTS); d < -5*time.Minute || d > 5*time.Minute {
		http.Error(w, "timestamp out of acceptable window", http.StatusUnauthorized)
		return
	}

	pubkeyBytes, err := base58.Decode(pubkeyB58)
	if err != nil || len(pubkeyBytes) != ed25519.PublicKeySize {
		http.Error(w, "invalid X-DoubleZero-Pubkey", http.StatusUnauthorized)
		return
	}
	sigBytes, err := base58.Decode(sigB58)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		http.Error(w, "invalid X-DoubleZero-Signature", http.StatusUnauthorized)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(rawBody))

	hash := sha256.Sum256(rawBody)
	bodyHashHex := hex.EncodeToString(hash[:])

	canonical := fmt.Sprintf(
		"DOUBLEZERO_STATE_PUSH_V1\nmethod:%s\npath:%s\ntimestamp:%s\nbody-sha256:%s\n",
		r.Method,
		r.URL.Path,
		tsHeader,
		bodyHashHex,
	)

	if !ed25519.Verify(ed25519.PublicKey(pubkeyBytes), []byte(canonical), sigBytes) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var req PushRequest
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.Metadata.DevicePubkey == "" || req.Metadata.SnapshotTimestamp == "" {
		http.Error(w, "missing metadata.device_pubkey or metadata.snapshot_timestamp", http.StatusBadRequest)
		return
	}
	if req.Metadata.DevicePubkey != pubkeyB58 {
		http.Error(w, "device_pubkey mismatch", http.StatusUnauthorized)
		return
	}

	ts, err := time.Parse(time.RFC3339, req.Metadata.SnapshotTimestamp)
	if err != nil {
		http.Error(w, "invalid metadata.snapshot_timestamp (expect RFC3339)", http.StatusBadRequest)
		return
	}
	ts = ts.UTC()
	date := ts.Format("2006-01-02")
	hour := ts.Format("15")
	filename := ts.Format("20060102T150405Z") + ".json"

	kind := kindOverride
	if kind == "" {
		kind = r.URL.Query().Get("kind")
	}
	if kind == "" {
		kind = req.Metadata.Kind
	}
	if kind == "" {
		kind = req.Metadata.Command
	}
	kind = sanitizePathComponent(kind)

	device := sanitizePathComponent(req.Metadata.DevicePubkey)

	keyPrefix := fmt.Sprintf("state/%s/device=%s/date=%s/hour=%s/", kind, device, date, hour)
	if s.prefix != "" {
		keyPrefix = s.prefix + "/" + keyPrefix
	}
	key := keyPrefix + filename

	_, err = s.s3.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(rawBody),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		log.Printf("s3 PutObject error: %v", err)
		http.Error(w, "failed to store payload", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"key":    key,
	})
}

func (s *Server) pushHandler(w http.ResponseWriter, r *http.Request) {
	s.handlePush(w, r, "")
}

func (s *Server) pushWithKindHandler(w http.ResponseWriter, r *http.Request) {
	const base = "/v1/push/"
	if !strings.HasPrefix(r.URL.Path, base) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, base)
	parts := strings.SplitN(rest, "/", 2)
	kind := parts[0]
	if kind == "" {
		http.Error(w, "missing kind in path", http.StatusBadRequest)
		return
	}
	s.handlePush(w, r, kind)
}

func main() {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		log.Fatal("S3_BUCKET is required")
	}
	prefix := os.Getenv("S3_PREFIX")

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}
	s3Client := s3.NewFromConfig(cfg)

	srv := &Server{
		s3:     s3Client,
		bucket: bucket,
		prefix: prefix,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/push", srv.pushHandler)
	mux.HandleFunc("/v1/push/", srv.pushWithKindHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
