package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
)

type Handler struct {
	log          *slog.Logger
	cfg          Config
	auth         *Authenticator
	svcReady     func() bool
	allowedKinds map[string]struct{}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeJSONError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, types.ErrorResponse{Error: msg, Code: status})
}

func NewHandler(log *slog.Logger, cfg Config, svc *ServiceabilityView) (*Handler, error) {
	if log == nil {
		return nil, errors.New("logger is required")
	}
	if svc == nil {
		return nil, errors.New("serviceability view is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("handler config validation failed: %w", err)
	}

	auth := &Authenticator{
		Clock: cfg.Clock,
		Skew:  cfg.AuthTimeSkew,
		LookupDevice: func(devicePK string) (serviceability.Device, bool) {
			return svc.GetDevice(devicePK)
		},
	}
	if err := auth.Validate(); err != nil {
		return nil, fmt.Errorf("authenticator validation failed: %w", err)
	}

	allowedKinds := make(map[string]struct{}, len(cfg.StateToCollectShowCommands)+len(cfg.StateToCollectCustom))
	for kind := range cfg.StateToCollectShowCommands {
		allowedKinds[kind] = struct{}{}
	}
	for _, kind := range cfg.StateToCollectCustom {
		allowedKinds[kind] = struct{}{}
	}

	return &Handler{
		log:          log,
		cfg:          cfg,
		auth:         auth,
		svcReady:     svc.Ready,
		allowedKinds: allowedKinds,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(types.HealthzPath, h.healthzHandler)
	mux.HandleFunc(types.ReadyzPath, h.readyzHandler)
	mux.HandleFunc(types.UploadURLPath, h.uploadURLHandler)
	mux.HandleFunc(types.StateToCollectPath, h.stateToCollectHandler)
}

func (h *Handler) uploadURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		UploadRequestErrorsTotal.WithLabelValues("method_not_allowed", "unknown", "unknown").Inc()
		return
	}

	if h.svcReady != nil && !h.svcReady() {
		retryAfter := max(int(h.cfg.ServiceabilityRefreshInterval.Seconds()), 1)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		h.writeJSONError(w, http.StatusServiceUnavailable, "not ready")
		UploadRequestErrorsTotal.WithLabelValues("not_ready", "unknown", "unknown").Inc()
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			h.writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
			UploadRequestErrorsTotal.WithLabelValues("request_body_too_large", "unknown", "unknown").Inc()
			return
		}
		h.writeJSONError(w, http.StatusBadRequest, "failed to read body")
		UploadRequestErrorsTotal.WithLabelValues("failed_to_read_body", "unknown", "unknown").Inc()
		return
	}

	authed, err := h.auth.Authenticate(r, body)
	if err != nil {
		h.writeAuthError(w, err)
		UploadRequestErrorsTotal.WithLabelValues("auth_error", "unknown", "unknown").Inc()
		return
	}

	var req types.UploadURLRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid json")
		UploadRequestErrorsTotal.WithLabelValues("invalid_json", "unknown", "unknown").Inc()
		return
	}

	if strings.TrimSpace(req.DevicePubkey) == "" || req.DevicePubkey != authed.DevicePK {
		h.writeJSONError(w, http.StatusUnauthorized, "device pubkey mismatch")
		UploadRequestErrorsTotal.WithLabelValues("device_pubkey_mismatch", authed.DevicePK, "unknown").Inc()
		return
	}

	if req.SnapshotTimestamp == "" || req.SnapshotSHA256 == "" || req.Kind == "" {
		h.writeJSONError(w, http.StatusBadRequest, "missing fields")
		UploadRequestErrorsTotal.WithLabelValues("missing_fields", authed.DevicePK, "unknown").Inc()
		return
	}

	if !isValidSHA256Hex(req.SnapshotSHA256) {
		h.writeJSONError(w, http.StatusBadRequest, "invalid snapshot_sha256")
		UploadRequestErrorsTotal.WithLabelValues("invalid_snapshot_sha256", authed.DevicePK, "unknown").Inc()
		return
	}

	if _, ok := h.allowedKinds[req.Kind]; !ok {
		h.writeJSONError(w, http.StatusBadRequest, "invalid kind")
		UploadRequestErrorsTotal.WithLabelValues("invalid_kind", authed.DevicePK, "unknown").Inc()
		return
	}

	snapTS, err := time.Parse(time.RFC3339, req.SnapshotTimestamp)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid snapshot_timestamp (expect RFC3339)")
		UploadRequestErrorsTotal.WithLabelValues("invalid_timestamp", authed.DevicePK, req.Kind).Inc()
		return
	}
	snapTS = snapTS.UTC()

	key := buildSnapshotKey(h.cfg.BucketPathPrefix, req, snapTS)

	psReq, err := h.presignPut(r.Context(), key)
	if err != nil {
		h.log.Error("failed to create upload url", "error", err)
		h.writeJSONError(w, http.StatusInternalServerError, "failed to create upload url")
		UploadRequestErrorsTotal.WithLabelValues("failed_to_create_upload_url", authed.DevicePK, req.Kind).Inc()
		return
	}

	resp := types.UploadURLResponse{
		Status: "ok",
		S3Key:  key,
	}
	resp.Upload.Method = http.MethodPut
	resp.Upload.URL = psReq.URL
	resp.Upload.Headers = filterPresignHeaders(psReq.SignedHeader)

	UploadRequestsTotal.WithLabelValues(req.Kind, authed.DevicePK).Inc()

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

func (h *Handler) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ready := true
	if h.svcReady != nil {
		ready = h.svcReady()
	}

	w.Header().Set("Content-Type", "application/json")
	if !ready {
		retryAfter := max(int(h.cfg.ServiceabilityRefreshInterval.Seconds()), 1)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "not_ready",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ready",
	})
}

func (h *Handler) stateToCollectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		h.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	custom := make([]string, 0, len(h.cfg.StateToCollectCustom))
	for _, cmd := range h.cfg.StateToCollectCustom {
		custom = append(custom, cmd)
	}

	showCommands := make([]types.ShowCommand, 0, len(h.cfg.StateToCollectShowCommands))
	for kind, cmd := range h.cfg.StateToCollectShowCommands {
		showCommands = append(showCommands, types.ShowCommand{
			Kind:    kind,
			Command: cmd,
		})
	}

	resp := types.StateToCollectResponse{
		ShowCommands: showCommands,
		Custom:       custom,
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) presignPut(ctx context.Context, key string) (*awssigner.PresignedHTTPRequest, error) {
	return h.cfg.Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(h.cfg.BucketName),
		Key:         aws.String(key),
		ContentType: aws.String("application/json"),
	}, func(o *s3.PresignOptions) { o.Expires = h.cfg.PresignTTL })
}

func (h *Handler) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrMissingAuthHeaders):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrInvalidTimestamp):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrTimestampOutsideWindow):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrDeviceNotAuthorized):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrInvalidSignatureEncoding):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrInvalidSignature):
		h.writeJSONError(w, http.StatusUnauthorized, err.Error())
	default:
		h.log.Error("unexpected auth error", "error", err)
		h.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
	}
}

func buildSnapshotKey(prefix string, req types.UploadURLRequest, snapTS time.Time) string {
	snapTS = snapTS.UTC()

	date := snapTS.Format("2006-01-02")
	hour := snapTS.Format("15")

	// RFC3339-like but safe for filenames and downstream tooling
	// e.g. 20251214T123456Z
	filenameTS := snapTS.Format("20060102T150405Z")

	key := "snapshots/" + req.Kind +
		"/device=" + req.DevicePubkey +
		"/date=" + date +
		"/hour=" + hour +
		"/" + filenameTS + ".json"

	if prefix != "" {
		key = prefix + "/" + key
	}
	return key
}

func isValidSHA256Hex(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

func filterPresignHeaders(hdr http.Header) map[string][]string {
	out := make(map[string][]string)
	for k, vs := range hdr {
		ck := http.CanonicalHeaderKey(k)
		switch ck {
		case "Host", "Content-Length":
			continue
		}
		if ck == "Content-Type" || strings.HasPrefix(strings.ToLower(ck), "x-amz-") {
			out[ck] = append([]string(nil), vs...)
		}
	}
	return out
}
