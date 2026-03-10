package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// apiBaseURL points to the stable Cloudflare v4 API origin used by both the
// DNS and Email Routing integrations.
const apiBaseURL = "https://api.cloudflare.com/client/v4"

// Client is a minimal Cloudflare API client built on the standard library so
// the backend keeps external dependencies small and auditable.
type Client struct {
	httpClient *http.Client
	apiToken   string
}

// Zone is the compact zone shape returned by Cloudflare when the backend needs
// to resolve a root domain into its zone identifier.
type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
}

// DNSRecord mirrors the subset of Cloudflare DNS record fields used by the DNS
// management pages and service layer.
type DNSRecord struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Comment  string `json:"comment"`
	Priority *int   `json:"priority,omitempty"`
}

// CreateDNSRecordInput describes the payload sent to Cloudflare when the
// backend creates one DNS record.
type CreateDNSRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Comment  string `json:"comment,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// UpdateDNSRecordInput intentionally reuses the exact same payload as the
// create flow because Cloudflare expects the full record body on updates.
type UpdateDNSRecordInput = CreateDNSRecordInput

// EmailRoutingAddress mirrors Cloudflare Email Routing destination addresses.
// The verified timestamp is nil until the target mailbox owner confirms the
// verification email sent by Cloudflare.
type EmailRoutingAddress struct {
	ID       string     `json:"id"`
	Created  *time.Time `json:"created,omitempty"`
	Email    string     `json:"email"`
	Modified *time.Time `json:"modified,omitempty"`
	Tag      string     `json:"tag,omitempty"`
	Verified *time.Time `json:"verified,omitempty"`
}

// CreateEmailRoutingAddressInput is the documented request body for creating a
// destination address under one Cloudflare account.
type CreateEmailRoutingAddressInput struct {
	Email string `json:"email"`
}

// EmailRoutingRuleAction mirrors one Email Routing rule action. LinuxDoSpace
// currently only uses the documented "forward" action.
type EmailRoutingRuleAction struct {
	Type  string   `json:"type"`
	Value []string `json:"value,omitempty"`
}

// EmailRoutingRuleMatcher mirrors one Email Routing matcher. LinuxDoSpace uses
// the documented literal matcher on the "to" field so every mailbox address is
// synced as one exact Cloudflare route.
type EmailRoutingRuleMatcher struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

// EmailRoutingRule mirrors the Cloudflare rule object returned by the Email
// Routing rules API.
type EmailRoutingRule struct {
	ID       string                    `json:"id"`
	Actions  []EmailRoutingRuleAction  `json:"actions,omitempty"`
	Enabled  bool                      `json:"enabled"`
	Matchers []EmailRoutingRuleMatcher `json:"matchers,omitempty"`
	Name     string                    `json:"name,omitempty"`
	Priority int                       `json:"priority,omitempty"`
	Tag      string                    `json:"tag,omitempty"`
}

// EmailRoutingDNSRecord mirrors the DNS records Cloudflare requires for Email
// Routing on either the zone root or one routed subdomain namespace.
type EmailRoutingDNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Proxied  bool   `json:"proxied,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// EmailRoutingDNSSettings captures the Email Routing DNS configuration payload
// returned by Cloudflare when the API includes the required DNS records inline.
type EmailRoutingDNSSettings struct {
	Name    string                  `json:"name,omitempty"`
	Enabled bool                    `json:"enabled,omitempty"`
	Status  string                  `json:"status,omitempty"`
	Records []EmailRoutingDNSRecord `json:"records,omitempty"`
}

// Identifier returns the stable rule identifier expected by Cloudflare route
// update and delete endpoints. The API still exposes the legacy tag field on
// some responses, so the helper falls back to it when needed.
func (r EmailRoutingRule) Identifier() string {
	if strings.TrimSpace(r.ID) != "" {
		return strings.TrimSpace(r.ID)
	}
	return strings.TrimSpace(r.Tag)
}

// CreateEmailRoutingRuleInput is the documented request body for creating one
// Email Routing rule under a zone.
type CreateEmailRoutingRuleInput struct {
	Actions  []EmailRoutingRuleAction  `json:"actions"`
	Matchers []EmailRoutingRuleMatcher `json:"matchers"`
	Enabled  bool                      `json:"enabled"`
	Name     string                    `json:"name,omitempty"`
	Priority int                       `json:"priority,omitempty"`
}

// UpdateEmailRoutingRuleInput intentionally matches the create payload because
// Cloudflare's update API expects the same rule shape.
type UpdateEmailRoutingRuleInput = CreateEmailRoutingRuleInput

// The aliases below preserve the naming already used by the service layer while
// keeping the concrete request and response shapes aligned with the Cloudflare
// API documentation.
type EmailRoutingDestinationAddress = EmailRoutingAddress
type EmailRoutingAction = EmailRoutingRuleAction
type EmailRoutingMatcher = EmailRoutingRuleMatcher
type UpsertEmailRoutingRuleInput = CreateEmailRoutingRuleInput

// emailRoutingDNSResponse is the Cloudflare Email Routing DNS envelope. The
// API currently returns either a direct record list or an object that embeds
// the list, so the result is decoded in a second step.
type emailRoutingDNSResponse struct {
	Result  json.RawMessage      `json:"result"`
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
}

// cfSuccess lets the DNS response participate in the shared Cloudflare error
// handling inside doJSON.
func (r *emailRoutingDNSResponse) cfSuccess() bool {
	return r.Success
}

// cfErrors returns the Cloudflare error list from one Email Routing DNS call.
func (r *emailRoutingDNSResponse) cfErrors() []cloudflareAPIError {
	return r.Errors
}

// cloudflareAPIError captures the shared Cloudflare error shape used across the
// DNS and Email Routing endpoints.
type cloudflareAPIError struct {
	Message string `json:"message"`
}

// listResponse is the shared paginated Cloudflare response envelope.
type listResponse[T any] struct {
	Result     []T                  `json:"result"`
	Success    bool                 `json:"success"`
	Errors     []cloudflareAPIError `json:"errors"`
	ResultInfo struct {
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// cfSuccess lets the generic response envelope participate in unified error
// handling inside doJSON.
func (r *listResponse[T]) cfSuccess() bool {
	return r.Success
}

// cfErrors returns the Cloudflare error list from a paginated response.
func (r *listResponse[T]) cfErrors() []cloudflareAPIError {
	return r.Errors
}

// objectResponse is the shared single-result Cloudflare response envelope.
type objectResponse[T any] struct {
	Result  T                    `json:"result"`
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
}

// cfSuccess lets the single-object response envelope participate in unified
// error handling inside doJSON.
func (r *objectResponse[T]) cfSuccess() bool {
	return r.Success
}

// cfErrors returns the Cloudflare error list from a single-object response.
func (r *objectResponse[T]) cfErrors() []cloudflareAPIError {
	return r.Errors
}

// cloudflareResponse is the narrow contract required by doJSON to decode a
// response and surface API-level errors consistently.
type cloudflareResponse interface {
	cfSuccess() bool
	cfErrors() []cloudflareAPIError
}

// NewClient creates a Cloudflare API client with a conservative timeout that is
// reused for both DNS and Email Routing requests.
func NewClient(apiToken string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		apiToken:   strings.TrimSpace(apiToken),
	}
}

// ResolveZoneID looks up the Cloudflare zone identifier for one root domain.
func (c *Client) ResolveZoneID(ctx context.Context, rootDomain string) (string, error) {
	if strings.TrimSpace(rootDomain) == "" {
		return "", fmt.Errorf("root domain is required")
	}

	values := url.Values{}
	values.Set("name", rootDomain)

	var response listResponse[Zone]
	if err := c.doJSON(ctx, http.MethodGet, apiBaseURL+"/zones?"+values.Encode(), nil, &response); err != nil {
		return "", err
	}

	if len(response.Result) == 0 {
		return "", fmt.Errorf("zone %q not found", rootDomain)
	}

	return response.Result[0].ID, nil
}

// ResolveZone returns the first zone that exactly matches the provided root
// domain. The service layer uses this when it also needs the owning account id.
func (c *Client) ResolveZone(ctx context.Context, rootDomain string) (Zone, error) {
	if strings.TrimSpace(rootDomain) == "" {
		return Zone{}, fmt.Errorf("root domain is required")
	}

	values := url.Values{}
	values.Set("name", rootDomain)

	var response listResponse[Zone]
	if err := c.doJSON(ctx, http.MethodGet, apiBaseURL+"/zones?"+values.Encode(), nil, &response); err != nil {
		return Zone{}, err
	}
	if len(response.Result) == 0 {
		return Zone{}, fmt.Errorf("zone %q not found", rootDomain)
	}
	return response.Result[0], nil
}

// GetZone returns one zone by its identifier.
func (c *Client) GetZone(ctx context.Context, zoneID string) (Zone, error) {
	var response objectResponse[Zone]
	endpoint := fmt.Sprintf("%s/zones/%s", apiBaseURL, zoneID)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return Zone{}, err
	}
	return response.Result, nil
}

// ListAllDNSRecords fetches every DNS record in a zone and transparently walks
// through Cloudflare pagination.
func (c *Client) ListAllDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	if strings.TrimSpace(zoneID) == "" {
		return nil, fmt.Errorf("zone id is required")
	}

	all := make([]DNSRecord, 0, 32)
	page := 1

	for {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		values.Set("per_page", "100")

		var response listResponse[DNSRecord]
		endpoint := fmt.Sprintf("%s/zones/%s/dns_records?%s", apiBaseURL, zoneID, values.Encode())
		if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
			return nil, err
		}

		all = append(all, response.Result...)
		if page >= response.ResultInfo.TotalPages || response.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return all, nil
}

// GetDNSRecord returns one DNS record by its Cloudflare identifier.
func (c *Client) GetDNSRecord(ctx context.Context, zoneID string, recordID string) (DNSRecord, error) {
	var response objectResponse[DNSRecord]
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", apiBaseURL, zoneID, recordID)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return DNSRecord{}, err
	}
	return response.Result, nil
}

// CreateDNSRecord creates one DNS record inside the target zone.
func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, input CreateDNSRecordInput) (DNSRecord, error) {
	var response objectResponse[DNSRecord]
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records", apiBaseURL, zoneID)
	if err := c.doJSON(ctx, http.MethodPost, endpoint, input, &response); err != nil {
		return DNSRecord{}, err
	}
	return response.Result, nil
}

// UpdateDNSRecord fully replaces one DNS record in Cloudflare.
func (c *Client) UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, input UpdateDNSRecordInput) (DNSRecord, error) {
	var response objectResponse[DNSRecord]
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", apiBaseURL, zoneID, recordID)
	if err := c.doJSON(ctx, http.MethodPut, endpoint, input, &response); err != nil {
		return DNSRecord{}, err
	}
	return response.Result, nil
}

// DeleteDNSRecord removes one DNS record from Cloudflare.
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error {
	var response objectResponse[map[string]any]
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", apiBaseURL, zoneID, recordID)
	return c.doJSON(ctx, http.MethodDelete, endpoint, nil, &response)
}

// ListEmailRoutingAddresses returns every destination address available to the
// configured Cloudflare account.
func (c *Client) ListEmailRoutingAddresses(ctx context.Context, accountID string) ([]EmailRoutingAddress, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("account id is required")
	}

	all := make([]EmailRoutingAddress, 0, 16)
	page := 1

	for {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		values.Set("per_page", "100")

		var response listResponse[EmailRoutingAddress]
		endpoint := fmt.Sprintf("%s/accounts/%s/email/routing/addresses?%s", apiBaseURL, accountID, values.Encode())
		if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
			return nil, err
		}

		all = append(all, response.Result...)
		if page >= response.ResultInfo.TotalPages || response.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return all, nil
}

// CreateEmailRoutingAddress registers one destination address under the target
// Cloudflare account. Cloudflare sends a verification email when needed.
func (c *Client) CreateEmailRoutingAddress(ctx context.Context, accountID string, input CreateEmailRoutingAddressInput) (EmailRoutingAddress, error) {
	var response objectResponse[EmailRoutingAddress]
	endpoint := fmt.Sprintf("%s/accounts/%s/email/routing/addresses", apiBaseURL, accountID)
	if err := c.doJSON(ctx, http.MethodPost, endpoint, input, &response); err != nil {
		return EmailRoutingAddress{}, err
	}
	return response.Result, nil
}

// ListEmailRoutingDestinationAddresses preserves the earlier service-layer name
// for the same documented Cloudflare destination-address endpoint.
func (c *Client) ListEmailRoutingDestinationAddresses(ctx context.Context, accountID string) ([]EmailRoutingDestinationAddress, error) {
	return c.ListEmailRoutingAddresses(ctx, accountID)
}

// CreateEmailRoutingDestinationAddress preserves the earlier service-layer name
// while still using the documented Cloudflare request body.
func (c *Client) CreateEmailRoutingDestinationAddress(ctx context.Context, accountID string, email string) (EmailRoutingDestinationAddress, error) {
	return c.CreateEmailRoutingAddress(ctx, accountID, CreateEmailRoutingAddressInput{Email: email})
}

// EnableEmailRoutingDNS ensures Cloudflare Email Routing is enabled for the
// zone and returns any root-level DNS records Cloudflare wants present.
func (c *Client) EnableEmailRoutingDNS(ctx context.Context, zoneID string) ([]EmailRoutingDNSRecord, error) {
	endpoint := fmt.Sprintf("%s/zones/%s/email/routing/dns", apiBaseURL, zoneID)
	return c.doEmailRoutingDNSRequest(ctx, http.MethodPost, endpoint)
}

// ListEmailRoutingDNSRecords returns the DNS records Cloudflare requires for
// Email Routing on either the root zone or one routed subdomain namespace. For
// subdomain-scoped reads, Cloudflare expects the full FQDN such as
// `alice.linuxdo.space`, not only the relative label `alice`.
func (c *Client) ListEmailRoutingDNSRecords(ctx context.Context, zoneID string, subdomain string) ([]EmailRoutingDNSRecord, error) {
	values := url.Values{}
	if strings.TrimSpace(subdomain) != "" {
		values.Set("subdomain", strings.TrimSpace(subdomain))
	}

	endpoint := fmt.Sprintf("%s/zones/%s/email/routing/dns", apiBaseURL, zoneID)
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return c.doEmailRoutingDNSRequest(ctx, http.MethodGet, endpoint)
}

// ListAllEmailRoutingRules returns every Email Routing rule configured under a
// zone and transparently walks through Cloudflare pagination.
func (c *Client) ListAllEmailRoutingRules(ctx context.Context, zoneID string) ([]EmailRoutingRule, error) {
	if strings.TrimSpace(zoneID) == "" {
		return nil, fmt.Errorf("zone id is required")
	}

	all := make([]EmailRoutingRule, 0, 16)
	page := 1

	for {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		values.Set("per_page", "100")

		var response listResponse[EmailRoutingRule]
		endpoint := fmt.Sprintf("%s/zones/%s/email/routing/rules?%s", apiBaseURL, zoneID, values.Encode())
		if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
			return nil, err
		}

		all = append(all, response.Result...)
		if page >= response.ResultInfo.TotalPages || response.ResultInfo.TotalPages == 0 {
			break
		}
		page++
	}

	return all, nil
}

// ListEmailRoutingRules preserves the service-layer naming already used by the
// Email Routing synchronization helper.
func (c *Client) ListEmailRoutingRules(ctx context.Context, zoneID string) ([]EmailRoutingRule, error) {
	return c.ListAllEmailRoutingRules(ctx, zoneID)
}

// CreateEmailRoutingRule creates one Email Routing rule under the target zone.
func (c *Client) CreateEmailRoutingRule(ctx context.Context, zoneID string, input CreateEmailRoutingRuleInput) (EmailRoutingRule, error) {
	var response objectResponse[EmailRoutingRule]
	endpoint := fmt.Sprintf("%s/zones/%s/email/routing/rules", apiBaseURL, zoneID)
	if err := c.doJSON(ctx, http.MethodPost, endpoint, input, &response); err != nil {
		return EmailRoutingRule{}, err
	}
	return response.Result, nil
}

// UpdateEmailRoutingRule replaces one existing Email Routing rule.
func (c *Client) UpdateEmailRoutingRule(ctx context.Context, zoneID string, ruleID string, input UpdateEmailRoutingRuleInput) (EmailRoutingRule, error) {
	var response objectResponse[EmailRoutingRule]
	endpoint := fmt.Sprintf("%s/zones/%s/email/routing/rules/%s", apiBaseURL, zoneID, ruleID)
	if err := c.doJSON(ctx, http.MethodPut, endpoint, input, &response); err != nil {
		return EmailRoutingRule{}, err
	}
	return response.Result, nil
}

// DeleteEmailRoutingRule removes one Email Routing rule from the target zone.
func (c *Client) DeleteEmailRoutingRule(ctx context.Context, zoneID string, ruleID string) error {
	var response objectResponse[map[string]any]
	endpoint := fmt.Sprintf("%s/zones/%s/email/routing/rules/%s", apiBaseURL, zoneID, ruleID)
	return c.doJSON(ctx, http.MethodDelete, endpoint, nil, &response)
}

// doJSON centralizes authentication, request encoding, response decoding, and
// Cloudflare API error translation.
func (c *Client) doJSON(ctx context.Context, method string, endpoint string, requestBody any, responseBody cloudflareResponse) error {
	var bodyReader *bytes.Reader
	if requestBody == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal cloudflare request: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("create cloudflare request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform cloudflare request: %w", err)
	}
	defer response.Body.Close()

	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode cloudflare response: %w", err)
	}

	if !responseBody.cfSuccess() {
		return fmt.Errorf("cloudflare api error: %s", firstCloudflareError(responseBody.cfErrors()))
	}

	return nil
}

// firstCloudflareError turns the first Cloudflare API error entry into a stable
// plain-text message for logs and upstream HTTP responses.
func firstCloudflareError(errors []cloudflareAPIError) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	if strings.TrimSpace(errors[0].Message) == "" {
		return "unknown error"
	}
	return errors[0].Message
}

// doEmailRoutingDNSRequest handles the Cloudflare Email Routing DNS endpoints,
// whose result shape differs slightly between enable and read operations.
func (c *Client) doEmailRoutingDNSRequest(ctx context.Context, method string, endpoint string) ([]EmailRoutingDNSRecord, error) {
	var response emailRoutingDNSResponse
	if err := c.doJSON(ctx, method, endpoint, nil, &response); err != nil {
		return nil, err
	}
	return parseEmailRoutingDNSRecords(response.Result)
}

// parseEmailRoutingDNSRecords normalizes the Cloudflare Email Routing DNS
// payload into one flat DNS record slice regardless of the exact envelope form.
func parseEmailRoutingDNSRecords(raw json.RawMessage) ([]EmailRoutingDNSRecord, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var direct []EmailRoutingDNSRecord
	if err := json.Unmarshal(trimmed, &direct); err == nil {
		return direct, nil
	}

	var settings EmailRoutingDNSSettings
	if err := json.Unmarshal(trimmed, &settings); err == nil {
		if settings.Records != nil {
			return settings.Records, nil
		}
		// `POST /zones/{zone_id}/email/routing/dns` returns the Email Routing
		// enablement object rather than a DNS record list. A successful response
		// still means the feature is enabled, so the caller should continue.
		if strings.TrimSpace(settings.Name) != "" || strings.TrimSpace(settings.Status) != "" || settings.Enabled {
			return nil, nil
		}
	}

	var single EmailRoutingDNSRecord
	if err := json.Unmarshal(trimmed, &single); err == nil && strings.TrimSpace(single.Type) != "" {
		return []EmailRoutingDNSRecord{single}, nil
	}

	return nil, fmt.Errorf("unsupported cloudflare email routing dns response shape")
}
