package artifacts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3Store speaks S3 with SigV4 directly. MinIO and AWS both work; the only
// difference is PathStyle, which MinIO requires.
//
// Rolling the signature by hand instead of pulling in the AWS SDK keeps the
// dependency surface at zero for a feature that is three requests wide.
type S3Store struct {
	endpoint  string // http://minio:9000 or https://s3.us-west-2.amazonaws.com
	bucket    string
	region    string
	accessKey string
	secretKey string
	pathStyle bool
	hc        *http.Client
}

type S3Options struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

func NewS3Store(o S3Options) *S3Store {
	if o.Region == "" {
		o.Region = "us-east-1"
	}
	return &S3Store{
		endpoint:  strings.TrimSuffix(o.Endpoint, "/"),
		bucket:    o.Bucket,
		region:    o.Region,
		accessKey: o.AccessKey,
		secretKey: o.SecretKey,
		pathStyle: o.PathStyle,
		hc:        &http.Client{Timeout: 2 * time.Minute},
	}
}

// EnsureBucket creates the bucket if it is missing. Safe to call on every boot.
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	if err := s.request(ctx, http.MethodHead, "", nil, "", nil); err == nil {
		return nil
	}
	return s.request(ctx, http.MethodPut, "", nil, "", nil)
}

func (s *S3Store) Put(ctx context.Context, key, contentType string, body []byte) error {
	return s.request(ctx, http.MethodPut, key, body, contentType, nil)
}

func (s *S3Store) PutBase64(ctx context.Context, key, contentType, b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("artifact %s: %w", key, err)
	}
	return s.Put(ctx, key, contentType, raw)
}

func (s *S3Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	var out []byte
	var ct string
	err := s.request(ctx, http.MethodGet, key, nil, "", func(resp *http.Response) error {
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		out = drain(resp.Body)
		ct = resp.Header.Get("Content-Type")
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if ct == "" {
		ct = guessType(key)
	}
	return out, ct, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	return s.request(ctx, http.MethodDelete, key, nil, "", nil)
}

func (s *S3Store) urlFor(key string) string {
	if s.pathStyle {
		return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, escapePath(key))
	}
	// virtual-host style: https://bucket.host/key
	u, err := url.Parse(s.endpoint)
	if err != nil {
		return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, escapePath(key))
	}
	u.Host = s.bucket + "." + u.Host
	u.Path = "/" + escapePath(key)
	return u.String()
}

func (s *S3Store) request(
	ctx context.Context,
	method, key string,
	body []byte,
	contentType string,
	handle func(*http.Response) error,
) error {
	target := s.urlFor(key)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if err := s.sign(req, body); err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("s3 %s %s: %w", method, key, err)
	}
	defer resp.Body.Close()

	if handle != nil {
		return handle(resp)
	}
	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return fmt.Errorf("s3 %s %s: %d: %s", method, key, resp.StatusCode, string(drain(resp.Body)))
	}
	_ = drain(resp.Body)
	return nil
}

// sign applies AWS Signature Version 4 with the payload hash inline (no
// streaming, no chunked signing — artefacts are small).
func (s *S3Store) sign(req *http.Request, body []byte) error {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	payloadHash := hex.EncodeToString(sha256sum(body))
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}

	signed, canonicalHeaders := canonicalise(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		req.URL.RawQuery,
		canonicalHeaders,
		signed,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, s.region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(sha256sum([]byte(canonicalRequest))),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, s.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, signed, signature))
	return nil
}

func canonicalise(req *http.Request) (signedHeaders, canonical string) {
	names := []string{"host"}
	values := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "content-length" {
			continue
		}
		names = append(names, lk)
		values[lk] = strings.TrimSpace(strings.Join(v, ","))
	}
	sort.Strings(names)
	var sb strings.Builder
	uniq := make([]string, 0, len(names))
	var prev string
	for _, n := range names {
		if n == prev {
			continue
		}
		prev = n
		uniq = append(uniq, n)
		sb.WriteString(n)
		sb.WriteString(":")
		sb.WriteString(values[n])
		sb.WriteString("\n")
	}
	return strings.Join(uniq, ";"), sb.String()
}

func canonicalURI(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return escapePathKeepSlashes(u.EscapedPath())
}

// escapePath encodes a key for use in a URL path, leaving "/" as a separator.
func escapePath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func escapePathKeepSlashes(p string) string { return p }

func sha256sum(b []byte) []byte {
	h := sha256.New()
	h.Write(b)
	return h.Sum(nil)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
