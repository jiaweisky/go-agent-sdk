package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Credentials is the AWS credential SigV4 signs with.
type Credentials struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// AuthInput is what a signing function sees.
type AuthInput struct {
	URL     string
	Body    string
	Headers map[string]string
}

// AuthFunc signs a request and returns its headers.
type AuthFunc func(input AuthInput) (map[string]string, error)

// SigV4 signs a request with AWS SigV4. It fails when credentials are nil.
func SigV4(credentials *Credentials) AuthFunc {
	return sigV4(credentials)
}

func sigV4(credentials *Credentials) AuthFunc {
	return func(input AuthInput) (map[string]string, error) {
		if credentials == nil {
			return nil, fmt.Errorf(
				"Bedrock Converse requires either route bearer auth or AWS credentials configured on the route",
			)
		}
		if input.Headers == nil {
			input.Headers = make(map[string]string)
		}
		input.Headers["content-type"] = "application/json"
		signed, err := signRequest(input, credentials)
		if err != nil {
			return nil, fmt.Errorf("Bedrock Converse SigV4 signing failed: %w", err)
		}
		for k, v := range signed {
			input.Headers[k] = v
		}
		return input.Headers, nil
	}
}

func signRequest(input AuthInput, creds *Credentials) (map[string]string, error) {
	bodyHash := sha256Hex(input.Body)
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	service := "bedrock"

	headers := signableHeaders(input, creds, amzDate, bodyHash)
	canonical, signedNames := canonicalRequest(headers, pathFromURL(input.URL), bodyHash)

	algorithm := "AWS4-HMAC-SHA256"
	credentialScope := dateStamp + "/" + creds.Region + "/" + service + "/aws4_request"
	stringToSign := algorithm + "\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		sha256Hex(canonical)

	signingKey := computeSigningKey(creds.SecretAccessKey, dateStamp, creds.Region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	authorization := algorithm + " " +
		"Credential=" + creds.AccessKeyID + "/" + credentialScope + ", " +
		"SignedHeaders=" + strings.Join(signedNames, ";") + ", " +
		"Signature=" + signature

	result := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		result[k] = v
	}
	result["authorization"] = authorization
	return result, nil
}

// signableHeaders copies the request headers and adds the SigV4 headers.
func signableHeaders(input AuthInput, creds *Credentials, amzDate, bodyHash string) map[string]string {
	headers := make(map[string]string)
	for k, v := range input.Headers {
		headers[strings.ToLower(k)] = v
	}
	headers["host"] = hostFromURL(input.URL)
	headers["x-amz-date"] = amzDate
	headers["x-amz-content-sha256"] = bodyHash
	if creds.SessionToken != "" {
		headers["x-amz-security-token"] = creds.SessionToken
	}
	return headers
}

// canonicalRequest builds the SigV4 canonical request and the sorted signed
// header names.
func canonicalRequest(headers map[string]string, path, bodyHash string) (string, []string) {
	signedNames := make([]string, 0, len(headers))
	for k := range headers {
		signedNames = append(signedNames, k)
	}
	sort.Strings(signedNames)
	canonicalHeaders := ""
	for _, k := range signedNames {
		canonicalHeaders += k + ":" + strings.TrimSpace(headers[k]) + "\n"
	}
	request := "POST" + "\n" +
		path + "\n" +
		"" + "\n" +
		canonicalHeaders + "\n" +
		strings.Join(signedNames, ";") + "\n" +
		bodyHash
	return request, signedNames
}

func hostFromURL(rawURL string) string {
	url := rawURL
	if idx := strings.Index(url, "://"); idx >= 0 {
		url = url[idx+3:]
	}
	if idx := strings.Index(url, "/"); idx >= 0 {
		url = url[:idx]
	}
	if idx := strings.Index(url, ":"); idx >= 0 {
		url = url[:idx]
	}
	return url
}

func pathFromURL(rawURL string) string {
	url := rawURL
	if idx := strings.Index(url, "://"); idx >= 0 {
		url = url[idx+3:]
	}
	if idx := strings.Index(url, "/"); idx >= 0 {
		return url[idx:]
	}
	return "/"
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func computeSigningKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	return kSigning
}
