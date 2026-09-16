package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

const adminCLIPasswordEnv = "PSV_ADMIN_PASSWORD"
const maxAdminCLIResponse = 2 << 20

var errAdminCLI = errors.New("admin command failed")

type adminCLIClient struct {
	base   *url.URL
	client *http.Client
	csrf   string
}

func runSession(args []string, stdout, stderr io.Writer, env map[string]string) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	command := args[0]
	if command != "list" && command != "show" && command != "rotate" && command != "delete" {
		return usageError(stderr)
	}
	f := flag.NewFlagSet("session "+command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	adminURL := f.String("admin", "http://127.0.0.1:9090", "Admin API base URL")
	username := f.String("username", "", "Admin username")
	jsonOutput := f.Bool("json", false, "Output machine-readable JSON")
	if f.Parse(args[1:]) != nil || *username == "" || (command == "list" && f.NArg() != 0) || (command != "list" && f.NArg() != 1) || (command == "delete" && *jsonOutput) {
		return usageError(stderr)
	}
	var id model.ID
	if command != "list" {
		id = model.ID(f.Arg(0))
		if !id.Valid() {
			return usageError(stderr)
		}
	}
	password := env[adminCLIPasswordEnv]
	if password == "" || len(password) > 4096 {
		_, _ = io.WriteString(stderr, "SESSION_AUTH_UNAVAILABLE: set PSV_ADMIN_PASSWORD for this command.\n")
		return 1
	}
	client, err := newAdminCLIClient(*adminURL)
	if err != nil || client.login(context.Background(), *username, password) != nil {
		_, _ = io.WriteString(stderr, "SESSION_AUTH_FAILED: verify the local Admin URL and credentials.\n")
		return 1
	}
	defer client.logout(context.Background())
	result, err := client.executeSession(context.Background(), command, id)
	if err != nil {
		_, _ = io.WriteString(stderr, "SESSION_COMMAND_FAILED: the requested session operation was not completed.\n")
		return 1
	}
	if command == "delete" {
		_, err = fmt.Fprintf(stdout, "session deleted: %s\n", id)
		if err != nil {
			return 1
		}
		return 0
	}
	if validateSessionPayload(command, result) != nil {
		_, _ = io.WriteString(stderr, "SESSION_COMMAND_FAILED: the Admin API returned invalid session data.\n")
		return 1
	}
	if *jsonOutput {
		var pretty bytes.Buffer
		if json.Indent(&pretty, result, "", "  ") != nil || pretty.WriteByte('\n') != nil {
			return 1
		}
		if _, err = stdout.Write(pretty.Bytes()); err != nil {
			return 1
		}
		return 0
	}
	return writeSessionSummary(stdout, command, result)
}

func validateSessionPayload(command string, body []byte) error {
	if command == "list" {
		var page struct {
			Items []publicsession.Session `json:"items"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return errAdminCLI
		}
		for _, entry := range page.Items {
			if entry.Validate() != nil {
				return errAdminCLI
			}
		}
		return nil
	}
	var entry publicsession.Session
	if json.Unmarshal(body, &entry) != nil || entry.Validate() != nil {
		return errAdminCLI
	}
	return nil
}

func newAdminCLIClient(raw string) (*adminCLIClient, error) {
	base, err := url.Parse(raw)
	if err != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" && base.Path != "/" || base.Hostname() == "" {
		return nil, errAdminCLI
	}
	if base.Scheme == "http" {
		host := strings.TrimSuffix(base.Hostname(), ".")
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, errAdminCLI
		}
	} else if base.Scheme != "https" {
		return nil, errAdminCLI
	}
	base.Path = strings.TrimSuffix(base.Path, "/")
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errAdminCLI
	}
	httpClient := &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &adminCLIClient{base: base, client: httpClient}, nil
}

func (c *adminCLIClient) login(ctx context.Context, username, password string) error {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return errAdminCLI
	}
	response, err := c.request(ctx, http.MethodPost, "/api/v1/auth/login", body, false)
	for i := range body {
		body[i] = 0
	}
	if err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			_ = response.Body.Close()
		}
		return errAdminCLI
	}
	_, err = readBounded(response)
	if err != nil {
		return err
	}
	for _, cookie := range c.client.Jar.Cookies(c.base) {
		if cookie.Name == "proxysieve_csrf" {
			c.csrf = cookie.Value
		}
	}
	if c.csrf == "" {
		return errAdminCLI
	}
	return nil
}

func (c *adminCLIClient) executeSession(ctx context.Context, command string, id model.ID) ([]byte, error) {
	method, path := http.MethodGet, "/api/v1/sessions"
	switch command {
	case "show":
		path += "/" + url.PathEscape(string(id))
	case "rotate":
		method, path = http.MethodPost, path+"/"+url.PathEscape(string(id))+"/rotate"
	case "delete":
		method, path = http.MethodDelete, path+"/"+url.PathEscape(string(id))
	}
	body := []byte(nil)
	if method != http.MethodGet {
		body = []byte("{}")
	}
	response, err := c.request(ctx, method, path, body, method != http.MethodGet)
	if err != nil {
		return nil, errAdminCLI
	}
	if command == "delete" {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusNoContent {
			return nil, errAdminCLI
		}
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, errAdminCLI
	}
	return readBounded(response)
}

func (c *adminCLIClient) logout(ctx context.Context) {
	response, err := c.request(ctx, http.MethodPost, "/api/v1/auth/logout", []byte("{}"), true)
	if err == nil {
		_ = response.Body.Close()
	}
}

func (c *adminCLIClient) request(ctx context.Context, method, path string, body []byte, mutation bool) (*http.Response, error) {
	target := *c.base
	target.Path = path
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errAdminCLI
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if mutation {
		request.Header.Set("X-CSRF-Token", c.csrf)
	}
	return c.client.Do(request)
}

func readBounded(response *http.Response) ([]byte, error) {
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAdminCLIResponse+1))
	if err != nil || len(body) > maxAdminCLIResponse || !json.Valid(body) {
		return nil, errAdminCLI
	}
	return body, nil
}

func writeSessionSummary(stdout io.Writer, command string, body []byte) int {
	if command == "list" {
		var page struct {
			Items []publicsession.Session `json:"items"`
		}
		if json.Unmarshal(body, &page) != nil {
			return 1
		}
		if len(page.Items) == 0 {
			if _, err := io.WriteString(stdout, "No runtime sessions.\n"); err != nil {
				return 1
			}
			return 0
		}
		for _, entry := range page.Items {
			if _, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", entry.ID, entry.Status, entry.ClientID, entry.PoolID, entry.ProxyEndpointID); err != nil {
				return 1
			}
		}
		return 0
	}
	var entry publicsession.Session
	if json.Unmarshal(body, &entry) != nil {
		return 1
	}
	_, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", entry.ID, entry.Status, entry.ClientID, entry.PoolID, entry.ProxyEndpointID)
	if err != nil {
		return 1
	}
	return 0
}
