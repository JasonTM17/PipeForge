package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type client struct {
	baseURL   string
	tokenFile string
	http      *http.Client
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	c := client{baseURL: envOr("PIPEFORGE_API_URL", "http://localhost:58080"), tokenFile: envOr("PIPEFORGE_TOKEN_FILE", ".pipeforge-token"), http: &http.Client{Timeout: 10 * time.Minute}}
	var err error
	switch os.Args[1] {
	case "login":
		err = c.login(os.Args[2:])
	case "datasets":
		err = c.printAuthed("GET", "/v1/datasets?page=1&pageSize=100", nil)
	case "jobs":
		err = c.printAuthed("GET", "/api/v1/jobs?page=1&pageSize=100", nil)
	case "job":
		err = c.printAuthed("GET", "/api/v1/jobs/"+requiredArg(os.Args[2:], "job id"), nil)
	case "artifacts":
		err = c.printAuthed("GET", "/api/v1/jobs/"+requiredArg(os.Args[2:], "job id")+"/artifacts?page=1&pageSize=100", nil)
	case "cancel":
		err = c.printAuthed("POST", "/api/v1/jobs/"+requiredArg(os.Args[2:], "job id")+"/cancel", []byte(`{}`))
	case "upload":
		err = c.upload(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipectl:", err)
		os.Exit(1)
	}
}

func (c client) login(args []string) error {
	set := flag.NewFlagSet("login", flag.ContinueOnError)
	email, password := set.String("email", "", "account email"), set.String("password", "", "account password")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *email == "" || *password == "" {
		return errors.New("--email and --password are required")
	}
	payload, _ := json.Marshal(map[string]string{"email": *email, "password": *password})
	body, err := c.request("POST", "/v1/auth/login", payload, false)
	if err != nil {
		return err
	}
	var tokens struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(body, &tokens); err != nil || tokens.AccessToken == "" {
		return errors.New("login response did not contain an access token")
	}
	if err := os.WriteFile(c.tokenFile, []byte(tokens.AccessToken+"\n"), 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	fmt.Println("authenticated; token saved to", c.tokenFile)
	return nil
}

func (c client) upload(args []string) error {
	set := flag.NewFlagSet("upload", flag.ContinueOnError)
	datasetID, file, contentType := set.String("dataset", "", "dataset id"), set.String("file", "", "local file"), set.String("content-type", "application/octet-stream", "upload content type")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *datasetID == "" || *file == "" {
		return errors.New("--dataset and --file are required")
	}
	payload, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	name := filepath.Base(*file)
	body, err := c.requestWithHeaders("POST", "/v1/datasets/"+*datasetID+"/versions", payload, true, map[string]string{"Content-Type": *contentType, "X-Filename": name})
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func (c client) printAuthed(method, path string, body []byte) error {
	response, err := c.request(method, path, body, true)
	if err != nil {
		return err
	}
	fmt.Println(string(response))
	return nil
}

func (c client) request(method, path string, body []byte, auth bool) ([]byte, error) {
	return c.requestWithHeaders(method, path, body, auth, map[string]string{"Content-Type": "application/json"})
}

func (c client) requestWithHeaders(method, path string, body []byte, auth bool, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest(method, strings.TrimRight(c.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if auth {
		token, err := c.token()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("api returned %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func (c client) token() (string, error) {
	if value := os.Getenv("PIPEFORGE_TOKEN"); value != "" {
		return value, nil
	}
	value, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file %q or set PIPEFORGE_TOKEN: %w", c.tokenFile, err)
	}
	token := strings.TrimSpace(string(value))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}

func requiredArg(args []string, name string) string {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, "missing", name)
		os.Exit(2)
	}
	return args[0]
}
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func usage() {
	fmt.Println("pipectl login --email EMAIL --password PASSWORD | datasets | jobs | job ID | artifacts JOB_ID | upload --dataset ID --file PATH | cancel JOB_ID")
}
