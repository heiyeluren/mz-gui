// Package ossv4 提供阿里云 OSS V4 请求签名适配器。
package ossv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	unsignedPayload = "UNSIGNED-PAYLOAD"
	algorithm       = "OSS4-HMAC-SHA256"
	service         = "oss"
	terminal        = "aliyun_v4_request"
)

var ErrInvalidConfiguration = errors.New("OSS V4 签名配置无效")

var reservedHeaders = map[string]struct{}{
	"authorization": {}, "date": {}, "host": {}, "x-oss-content-sha256": {}, "x-oss-date": {}, "x-oss-security-token": {},
}

type Credentials struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
}

type Options struct {
	Bucket          string
	ObjectKey       string
	Region          string
	Credentials     Credentials
	RequiredHeaders map[string]string
	ContentLength   int64
	SignTime        time.Time
}

// SignPUT 原地写入 PUT 请求的 OSS V4 签名头。调用方必须自行限制请求目标和跳转行为。
func SignPUT(request *http.Request, options Options) error {
	if request == nil || request.URL == nil || request.Method != http.MethodPut || options.ContentLength < 1 ||
		!validBucket(options.Bucket) || !validObjectKey(options.ObjectKey) {
		return ErrInvalidConfiguration
	}
	credentials := Credentials{
		AccessKeyID: strings.TrimSpace(options.Credentials.AccessKeyID), AccessKeySecret: strings.TrimSpace(options.Credentials.AccessKeySecret),
		SecurityToken: strings.TrimSpace(options.Credentials.SecurityToken),
	}
	if credentials.AccessKeyID == "" || credentials.AccessKeySecret == "" || credentials.SecurityToken == "" {
		return ErrInvalidConfiguration
	}
	region, err := NormalizeRegion(options.Region)
	if err != nil {
		return err
	}
	headers := make(map[string]string, len(options.RequiredHeaders)+4)
	originalNames := make(map[string]string, len(options.RequiredHeaders))
	for rawName, rawValue := range options.RequiredHeaders {
		name := strings.TrimSpace(rawName)
		lower := strings.ToLower(name)
		if !validHeaderName(name) {
			return ErrInvalidConfiguration
		}
		if _, reserved := reservedHeaders[lower]; reserved {
			return ErrInvalidConfiguration
		}
		if _, duplicate := originalNames[lower]; duplicate {
			return ErrInvalidConfiguration
		}
		value, ok := normalizeHeaderValue(rawValue)
		if !ok {
			return ErrInvalidConfiguration
		}
		headers[lower], originalNames[lower] = value, name
	}
	if returnedLength, exists := headers["content-length"]; exists && returnedLength != strconv.FormatInt(options.ContentLength, 10) {
		return ErrInvalidConfiguration
	}
	signTime := options.SignTime.UTC()
	if signTime.IsZero() {
		signTime = time.Now().UTC()
	}
	iso8601 := signTime.Format("20060102T150405Z")
	date := iso8601[:8]
	headers["x-oss-security-token"] = credentials.SecurityToken
	headers["x-oss-content-sha256"] = unsignedPayload
	headers["x-oss-date"] = iso8601
	headers["date"] = signTime.Format(http.TimeFormat)

	additional := make([]string, 0, len(headers))
	for name := range headers {
		if name != "content-md5" && name != "content-type" && !strings.HasPrefix(name, "x-oss-") && name != "date" {
			additional = append(additional, name)
		}
	}
	sort.Strings(additional)
	additionalSet := make(map[string]struct{}, len(additional))
	for _, name := range additional {
		additionalSet[name] = struct{}{}
	}
	canonicalNames := make([]string, 0, len(headers))
	for name := range headers {
		_, isAdditional := additionalSet[name]
		if name == "content-md5" || name == "content-type" || strings.HasPrefix(name, "x-oss-") || isAdditional {
			canonicalNames = append(canonicalNames, name)
		}
	}
	sort.Strings(canonicalNames)
	var canonicalHeaders strings.Builder
	for _, name := range canonicalNames {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(headers[name])
		canonicalHeaders.WriteByte('\n')
	}
	additionalNames := strings.Join(additional, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		percentEncode("/"+options.Bucket+"/"+options.ObjectKey, true),
		canonicalQuery(request.URL),
		canonicalHeaders.String(),
		additionalNames,
		unsignedPayload,
	}, "\n")
	scope := strings.Join([]string{date, region, service, terminal}, "/")
	digest := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{algorithm, iso8601, scope, hex.EncodeToString(digest[:])}, "\n")
	signingKey := hmacBytes(terminal,
		hmacBytes(service, hmacBytes(region, hmacBytes(date, []byte("aliyun_v4"+credentials.AccessKeySecret)))))
	signature := hex.EncodeToString(hmacBytes(stringToSign, signingKey))
	additionalAuthorization := ""
	if additionalNames != "" {
		additionalAuthorization = ",AdditionalHeaders=" + additionalNames
	}

	for lower, original := range originalNames {
		request.Header.Set(original, headers[lower])
	}
	request.ContentLength = options.ContentLength
	request.Header.Set("Content-Length", strconv.FormatInt(options.ContentLength, 10))
	request.Header.Set("x-oss-security-token", credentials.SecurityToken)
	request.Header.Set("x-oss-content-sha256", unsignedPayload)
	request.Header.Set("x-oss-date", iso8601)
	request.Header.Set("Date", headers["date"])
	request.Header.Set("Authorization", algorithm+" Credential="+credentials.AccessKeyID+"/"+scope+additionalAuthorization+",Signature="+signature)
	return nil
}

func NormalizeRegion(value string) (string, error) {
	region := strings.ToLower(strings.TrimSpace(value))
	region = strings.TrimPrefix(region, "oss-")
	if region == "" {
		return "", ErrInvalidConfiguration
	}
	for _, character := range region {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return "", ErrInvalidConfiguration
		}
	}
	return region, nil
}

func validBucket(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func validObjectKey(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "." || component == ".." {
			return false
		}
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func normalizeHeaderValue(value string) (string, bool) {
	if strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func canonicalQuery(value *url.URL) string {
	if value == nil || value.RawQuery == "" {
		return ""
	}
	items := make([][2]string, 0)
	for _, component := range strings.Split(value.RawQuery, "&") {
		parts := strings.SplitN(component, "=", 2)
		name, err := url.QueryUnescape(parts[0])
		if err != nil {
			return ""
		}
		encodedValue := ""
		if len(parts) == 2 {
			decoded, decodeErr := url.QueryUnescape(parts[1])
			if decodeErr != nil {
				return ""
			}
			encodedValue = percentEncode(decoded, false)
		}
		items = append(items, [2]string{percentEncode(name, false), encodedValue})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left][0] == items[right][0] {
			return items[left][1] < items[right][1]
		}
		return items[left][0] < items[right][0]
	})
	encoded := make([]string, 0, len(items))
	for _, item := range items {
		if item[1] == "" {
			encoded = append(encoded, item[0])
		} else {
			encoded = append(encoded, item[0]+"="+item[1])
		}
	}
	return strings.Join(encoded, "&")
}

func percentEncode(value string, preserveSlash bool) string {
	var result strings.Builder
	for _, current := range []byte(value) {
		if current >= 'A' && current <= 'Z' || current >= 'a' && current <= 'z' || current >= '0' && current <= '9' ||
			current == '-' || current == '.' || current == '_' || current == '~' || preserveSlash && current == '/' {
			result.WriteByte(current)
		} else {
			_, _ = fmt.Fprintf(&result, "%%%02X", current)
		}
	}
	return result.String()
}

func hmacBytes(value string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}
