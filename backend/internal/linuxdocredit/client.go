package linuxdocredit

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var (
	// ErrOrderNotFound reports the gateway's documented 404-style response for an
	// unknown or already-finished order query.
	ErrOrderNotFound = errors.New("linux.do credit order not found")

	// ErrInvalidSignature reports that one asynchronous gateway callback failed
	// the EasyPay-compatible MD5 verification step.
	ErrInvalidSignature = errors.New("linux.do credit signature is invalid")
)

// Client implements the EasyPay-compatible Linux Do Credit gateway contract.
type Client struct {
	pid        string
	key        string
	baseURL    string
	notifyURL  string
	returnURL  string
	httpClient *http.Client
}

// SubmitOrderRequest describes one local order that should be turned into an
// upstream Linux Do Credit checkout session.
type SubmitOrderRequest struct {
	OutTradeNo string
	Name       string
	Money      string
}

// SubmitOrderResult contains the gateway-hosted checkout URL created for one
// local order.
type SubmitOrderResult struct {
	PaymentURL string
}

// QueryOrderResult mirrors the documented order-query response fields that the
// backend needs for reconciliation.
type QueryOrderResult struct {
	TradeNo    string
	OutTradeNo string
	Name       string
	Money      string
	Status     int
	AddTime    string
	EndTime    string
}

// Notification describes one verified asynchronous success callback emitted by
// Linux Do Credit.
type Notification struct {
	PID         string
	TradeNo     string
	OutTradeNo  string
	Type        string
	Name        string
	Money       string
	TradeStatus string
}

type submitErrorResponse struct {
	ErrorMessage string `json:"error_msg"`
}

type queryOrderEnvelope struct {
	Code       int    `json:"code"`
	Message    string `json:"msg"`
	TradeNo    string `json:"trade_no"`
	OutTradeNo string `json:"out_trade_no"`
	Name       string `json:"name"`
	Money      string `json:"money"`
	Status     int    `json:"status"`
	AddTime    string `json:"addtime"`
	EndTime    string `json:"endtime"`
}

// NewClient builds one gateway client. Callers may pass empty credentials and
// then rely on Configured to keep the feature disabled.
func NewClient(pid string, key string, baseURL string, notifyURL string, returnURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Client{
		pid:        strings.TrimSpace(pid),
		key:        strings.TrimSpace(key),
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		notifyURL:  strings.TrimSpace(notifyURL),
		returnURL:  strings.TrimSpace(returnURL),
		httpClient: httpClient,
	}
}

// Configured reports whether the client has the minimum credentials required
// to talk to Linux Do Credit.
func (c *Client) Configured() bool {
	return c != nil && c.pid != "" && c.key != "" && c.baseURL != ""
}

// SubmitOrder creates one upstream checkout session and returns the payment URL
// that the browser should open.
func (c *Client) SubmitOrder(ctx context.Context, request SubmitOrderRequest) (SubmitOrderResult, error) {
	if !c.Configured() {
		return SubmitOrderResult{}, fmt.Errorf("linux.do credit is not configured")
	}

	form := url.Values{}
	form.Set("pid", c.pid)
	form.Set("type", "epay")
	form.Set("out_trade_no", strings.TrimSpace(request.OutTradeNo))
	form.Set("name", strings.TrimSpace(request.Name))
	form.Set("money", strings.TrimSpace(request.Money))
	if c.notifyURL != "" {
		form.Set("notify_url", c.notifyURL)
	}
	if c.returnURL != "" {
		form.Set("return_url", c.returnURL)
	}
	form.Set("sign_type", "MD5")
	form.Set("sign", SignValues(form, c.key))

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/pay/submit.php", strings.NewReader(form.Encode()))
	if err != nil {
		return SubmitOrderResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return SubmitOrderResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 && response.StatusCode < 400 {
		location := strings.TrimSpace(response.Header.Get("Location"))
		if location == "" {
			return SubmitOrderResult{}, fmt.Errorf("linux.do credit submit succeeded without a redirect location")
		}
		return SubmitOrderResult{PaymentURL: location}, nil
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return SubmitOrderResult{}, err
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return SubmitOrderResult{}, fmt.Errorf("linux.do credit submit returned HTTP %d without redirect: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var errorBody submitErrorResponse
	if json.Unmarshal(body, &errorBody) == nil && strings.TrimSpace(errorBody.ErrorMessage) != "" {
		return SubmitOrderResult{}, fmt.Errorf("linux.do credit submit failed: %s", strings.TrimSpace(errorBody.ErrorMessage))
	}

	return SubmitOrderResult{}, fmt.Errorf("linux.do credit submit failed with status %d", response.StatusCode)
}

// QueryOrder asks Linux Do Credit for the latest status of one business order.
func (c *Client) QueryOrder(ctx context.Context, outTradeNo string) (QueryOrderResult, error) {
	if !c.Configured() {
		return QueryOrderResult{}, fmt.Errorf("linux.do credit is not configured")
	}

	query := url.Values{}
	query.Set("act", "order")
	query.Set("pid", c.pid)
	query.Set("key", c.key)
	query.Set("out_trade_no", strings.TrimSpace(outTradeNo))

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api.php?"+query.Encode(), nil)
	if err != nil {
		return QueryOrderResult{}, err
	}
	httpRequest.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return QueryOrderResult{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return QueryOrderResult{}, err
	}

	if response.StatusCode == http.StatusNotFound {
		return QueryOrderResult{}, ErrOrderNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return QueryOrderResult{}, fmt.Errorf("linux.do credit query failed with status %d", response.StatusCode)
	}

	var envelope queryOrderEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return QueryOrderResult{}, fmt.Errorf("decode linux.do credit query response: %w", err)
	}
	if envelope.Code < 0 {
		if response.StatusCode == http.StatusNotFound {
			return QueryOrderResult{}, ErrOrderNotFound
		}
		return QueryOrderResult{}, fmt.Errorf("linux.do credit query failed: %s", strings.TrimSpace(envelope.Message))
	}

	return QueryOrderResult{
		TradeNo:    strings.TrimSpace(envelope.TradeNo),
		OutTradeNo: strings.TrimSpace(envelope.OutTradeNo),
		Name:       strings.TrimSpace(envelope.Name),
		Money:      normalizeMoneyString(envelope.Money),
		Status:     envelope.Status,
		AddTime:    strings.TrimSpace(envelope.AddTime),
		EndTime:    strings.TrimSpace(envelope.EndTime),
	}, nil
}

// VerifyNotification checks the callback signature and returns the normalized
// success payload when it is valid.
func (c *Client) VerifyNotification(values url.Values) (Notification, error) {
	if !c.Configured() {
		return Notification{}, fmt.Errorf("linux.do credit is not configured")
	}
	if !VerifySignedValues(values, c.key) {
		return Notification{}, ErrInvalidSignature
	}

	notification := Notification{
		PID:         strings.TrimSpace(values.Get("pid")),
		TradeNo:     strings.TrimSpace(values.Get("trade_no")),
		OutTradeNo:  strings.TrimSpace(values.Get("out_trade_no")),
		Type:        strings.TrimSpace(values.Get("type")),
		Name:        strings.TrimSpace(values.Get("name")),
		Money:       normalizeMoneyString(values.Get("money")),
		TradeStatus: strings.TrimSpace(values.Get("trade_status")),
	}

	if notification.PID != c.pid {
		return Notification{}, fmt.Errorf("linux.do credit callback pid mismatch")
	}
	if notification.Type != "epay" {
		return Notification{}, fmt.Errorf("linux.do credit callback type must be epay")
	}
	if notification.TradeStatus != "TRADE_SUCCESS" {
		return Notification{}, fmt.Errorf("linux.do credit callback trade_status must be TRADE_SUCCESS")
	}
	if notification.OutTradeNo == "" {
		return Notification{}, fmt.Errorf("linux.do credit callback out_trade_no is required")
	}
	if notification.Money == "" {
		return Notification{}, fmt.Errorf("linux.do credit callback money is required")
	}

	return notification, nil
}

// SignValues implements the documented EasyPay-compatible MD5 signing scheme.
func SignValues(values url.Values, secret string) string {
	pairs := make([]string, 0, len(values))
	for key := range values {
		if key == "sign" || key == "sign_type" {
			continue
		}
		value := strings.TrimSpace(values.Get(key))
		if value == "" {
			continue
		}
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)

	hash := md5.Sum([]byte(strings.Join(pairs, "&") + strings.TrimSpace(secret)))
	return hex.EncodeToString(hash[:])
}

// VerifySignedValues recomputes the MD5 signature over the provided query or
// form values and compares it against the transmitted `sign`.
func VerifySignedValues(values url.Values, secret string) bool {
	expected := SignValues(values, secret)
	actual := strings.ToLower(strings.TrimSpace(values.Get("sign")))
	return expected != "" && actual != "" && expected == actual
}

// normalizeMoneyString keeps the gateway's money field in a canonical,
// comparison-friendly format by trimming spaces and trailing zero noise.
func normalizeMoneyString(raw string) string {
	value := strings.TrimSpace(raw)
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "" {
		return "0"
	}
	return value
}
