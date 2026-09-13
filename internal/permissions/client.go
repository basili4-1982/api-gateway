package permissions

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	apiKey       string
	apiKeyHeader string
	method       string
	pathTemplate string
	httpClient   *http.Client
}

func NewClient(baseURL, apiKey, apiKeyHeader, method, pathTemplate string) *Client {
	return &Client{
		baseURL:      baseURL,
		apiKey:       apiKey,
		apiKeyHeader: apiKeyHeader,
		method:       method,
		pathTemplate: pathTemplate,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type UserPermissions struct {
	UserID            int      `json:"user_id"`
	Permissions       []string `json:"permissions"`
	InheritedFromRole []string `json:"inherited_from_role,omitempty"`
	DirectAllowed     []string `json:"direct_allowed,omitempty"`
	DirectDenied      []string `json:"direct_denied,omitempty"`
}

func (c *Client) GetEffectivePermissions(userID int) (*UserPermissions, error) {
	url := c.baseURL + strings.ReplaceAll(c.pathTemplate, "{user_id}", strconv.Itoa(userID))

	req, err := http.NewRequest(c.method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(c.apiKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to permission-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("permission-service returned %d: %s", resp.StatusCode, string(body))
	}

	var result UserPermissions
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}
