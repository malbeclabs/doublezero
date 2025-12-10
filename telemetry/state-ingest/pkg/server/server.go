package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mr-tron/base58"
)

const (
	authPrefix      = "DOUBLEZERO_V1"
	timeSkew        = 5 * time.Minute
	presignDuration = 15 * time.Minute
)

var AllowedKinds = map[string]struct{}{
	"snmp-mib-ifmib-ifindex": {},
}

type UploadURLRequest struct {
	DevicePubkey      string `json:"device_pubkey"`
	SnapshotTimestamp string `json:"snapshot_timestamp"`
	SnapshotSHA256    string `json:"snapshot_sha256"`
	Kind              string `json:"kind"`
}

type UploadURLResponse struct {
	Status string `json:"status"`
	S3Key  string `json:"s3_key"`
	Upload struct {
		Method  string              `json:"method"`
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
	} `json:"upload"`
}

type Server struct {
	log         *slog.Logger
	presign     *s3.PresignClient
	bucket      string
	prefix      string
	allowDevice func(ctx context.Context, pubkey string) bool
}

func New(log *slog.Logger, presign *s3.PresignClient, bucket, prefix string, allowDevice func(ctx context.Context, pubkey string) bool) (*Server, error) {
	s := &Server{
		log:         log,
		presign:     presign,
		bucket:      bucket,
		prefix:      prefix,
		allowDevice: allowDevice,
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context, cancel context.CancelFunc, listener net.Listener) <-chan error {
	errCh := make(chan error)
	go func() {
		defer close(errCh)
		defer cancel()
		if err := s.Run(ctx, listener); err != nil {
			s.log.Error("failed to start server", "error", err)
			errCh <- err
			return
		}
		s.log.Info("server stopped")
	}()
	return errCh
}

func (s *Server) Run(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/upload-url", s.uploadURLHandler)
	srv := &http.Server{
		Handler: mux,
	}
	return srv.Serve(listener)
}

func (s *Server) uploadURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pubkeyB58 := r.Header.Get("X-DZ-Pubkey")
	sigB58 := r.Header.Get("X-DZ-Signature")
	tsHeader := r.Header.Get("X-DZ-Timestamp")
	if pubkeyB58 == "" || sigB58 == "" || tsHeader == "" {
		http.Error(w, "missing auth headers", http.StatusUnauthorized)
		return
	}

	clientTS, err := time.Parse(time.RFC3339, tsHeader)
	if err != nil {
		http.Error(w, "invalid X-DZ-Timestamp", http.StatusUnauthorized)
		return
	}
	if d := time.Since(clientTS); d < -timeSkew || d > timeSkew {
		http.Error(w, "timestamp out of acceptable window", http.StatusUnauthorized)
		return
	}

	pubkeyBytes, err := base58.Decode(pubkeyB58)
	if err != nil || len(pubkeyBytes) != ed25519.PublicKeySize {
		http.Error(w, "invalid X-DZ-Pubkey", http.StatusUnauthorized)
		return
	}
	sigBytes, err := base58.Decode(sigB58)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		http.Error(w, "invalid X-DZ-Signature", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf(
		"%s\nmethod:%s\npath:%s\ntimestamp:%s\nbody-sha256:%x\n",
		authPrefix,
		r.Method,
		r.URL.Path,
		tsHeader,
		bodyHash[:],
	)
	if !ed25519.Verify(ed25519.PublicKey(pubkeyBytes), []byte(canonical), sigBytes) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	if s.allowDevice != nil && !s.allowDevice(r.Context(), pubkeyB58) {
		http.Error(w, "device not authorized", http.StatusForbidden)
		return
	}

	var req UploadURLRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.DevicePubkey == "" || req.DevicePubkey != pubkeyB58 {
		http.Error(w, "device_pubkey mismatch", http.StatusUnauthorized)
		return
	}
	pkBytes, err := base58.Decode(req.DevicePubkey)
	if err != nil || len(pkBytes) != 32 {
		http.Error(w, "invalid device_pubkey", http.StatusBadRequest)
		return
	}

	if req.SnapshotTimestamp == "" || req.SnapshotSHA256 == "" || req.Kind == "" {
		http.Error(w, "snapshot_timestamp, snapshot_sha256, and kind are required", http.StatusBadRequest)
		return
	}
	if _, ok := AllowedKinds[req.Kind]; !ok {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}

	snapTS, err := time.Parse(time.RFC3339, req.SnapshotTimestamp)
	if err != nil {
		http.Error(w, "invalid snapshot_timestamp (expect RFC3339)", http.StatusBadRequest)
		return
	}
	snapTS = snapTS.UTC()

	date := snapTS.Format("2006-01-02")
	hour := snapTS.Format("15")
	filename := fmt.Sprintf("%s.json", req.SnapshotSHA256)

	key := fmt.Sprintf(
		"snapshots/kind=%s/device=%s/date=%s/hour=%s/timestamp=%s/%s",
		req.Kind,
		req.DevicePubkey,
		date,
		hour,
		req.SnapshotTimestamp,
		filename,
	)
	if s.prefix != "" {
		key = s.prefix + "/" + key
	}

	psReq, err := s.presign.PresignPutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String("application/json"),
	}, func(o *s3.PresignOptions) { o.Expires = presignDuration })
	if err != nil {
		log.Printf("PresignPutObject error: %v", err)
		http.Error(w, "failed to create upload url", http.StatusInternalServerError)
		return
	}

	var resp UploadURLResponse
	resp.Status = "ok"
	resp.S3Key = key
	resp.Upload.Method = http.MethodPut
	resp.Upload.URL = psReq.URL
	resp.Upload.Headers = psReq.SignedHeader

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
