package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	GitHubToken    string   `json:"github_token"`
	SearchPatterns []string `json:"search_patterns"`
	FilePatterns   []string `json:"file_patterns"`
	RateLimit      int      `json:"rate_limit"`
}

type GitHubCodeSearchResult struct {
	Items []struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		HTMLURL string `json:"html_url"`
		Repo    struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"items"`
}

type Finding struct {
	Repository string `json:"repository"`
	FilePath   string `json:"file_path"`
	URL        string `json:"url"`
	Pattern    string `json:"pattern"`
	Severity   string `json:"severity"`
}

type RateLimitInfo struct {
	Limit     int `json:"limit"`
	Remaining int `json:"remaining"`
	Reset     int `json:"reset"`
}

type RequestStats struct {
	TotalRequests      int
	SuccessfulRequests int
	FailedRequests     int
	RateLimitHits      int
	mu                 sync.Mutex
}

type TokenPool struct {
	tokens  []string
	current int
	mu      sync.Mutex
}

func (tp *TokenPool) GetNextToken() string {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	token := tp.tokens[tp.current]
	tp.current = (tp.current + 1) % len(tp.tokens)
	return token
}

func (rs *RequestStats) IncrementTotal() {
	rs.mu.Lock()
	rs.TotalRequests++
	rs.mu.Unlock()
}

func (rs *RequestStats) IncrementSuccess() {
	rs.mu.Lock()
	rs.SuccessfulRequests++
	rs.mu.Unlock()
}

func (rs *RequestStats) IncrementFailed() {
	rs.mu.Lock()
	rs.FailedRequests++
	rs.mu.Unlock()
}

func (rs *RequestStats) IncrementRateLimit() {
	rs.mu.Lock()
	rs.RateLimitHits++
	rs.mu.Unlock()
}

func loadConfig(configPath string) (*Config, error) {
	file, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return &config, nil
}

func getRateLimitInfo(resp *http.Response) (*RateLimitInfo, error) {
	limit := resp.Header.Get("X-RateLimit-Limit")
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	reset := resp.Header.Get("X-RateLimit-Reset")

	if limit == "" || remaining == "" || reset == "" {
		return nil, fmt.Errorf("rate limit headers not found")
	}

	limitInt, _ := strconv.Atoi(limit)
	remainingInt, _ := strconv.Atoi(remaining)
	resetInt, _ := strconv.Atoi(reset)

	return &RateLimitInfo{
		Limit:     limitInt,
		Remaining: remainingInt,
		Reset:     resetInt,
	}, nil
}

func searchGitHub(ctx context.Context, config *Config, pattern string, stats *RequestStats) ([]Finding, error) {
	var allFindings []Finding
	page := 1
	perPage := 30 // Reduced for demo purposes

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nDemo timeout reached after 60 seconds!")
			return allFindings, nil
		default:
			url := fmt.Sprintf("https://api.github.com/search/code?q=%s+in:file&per_page=%d&page=%d",
				pattern, perPage, page)

			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return nil, fmt.Errorf("error creating request: %v", err)
			}

			req.Header.Set("User-Agent", "GitHubScanner-Demo")
			if config.GitHubToken != "" {
				req.Header.Set("Authorization", "token "+config.GitHubToken)
			}

			stats.IncrementTotal()
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				stats.IncrementFailed()
				return nil, fmt.Errorf("error making request: %v", err)
			}

			rateLimit, err := getRateLimitInfo(resp)
			if err == nil {
				fmt.Printf("API Calls: %d/%d remaining (resets in %d seconds)\n",
					rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset)

				// If we're running low on remaining calls, increase the delay
				if rateLimit.Remaining < 10 {
					waitTime := time.Duration(config.RateLimit*2) * time.Second
					fmt.Printf("Low on API calls, increasing delay to %v\n", waitTime)
					time.Sleep(waitTime)
				}
			}

			if resp.StatusCode == http.StatusForbidden {
				resp.Body.Close()
				stats.IncrementRateLimit()
				if rateLimit != nil && rateLimit.Remaining == 0 {
					resetTime := time.Unix(int64(rateLimit.Reset), 0)
					waitTime := time.Until(resetTime)
					fmt.Printf("Rate limit exceeded. Waiting %v before retrying...\n", waitTime)
					time.Sleep(waitTime)
					continue
				}
				return nil, fmt.Errorf("rate limit exceeded or unauthorized")
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				stats.IncrementFailed()
				return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}

			stats.IncrementSuccess()

			var result GitHubCodeSearchResult
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("error decoding response: %v", err)
			}
			resp.Body.Close()

			if len(result.Items) == 0 {
				break
			}

			for _, item := range result.Items {
				for _, filePattern := range config.FilePatterns {
					if matched, _ := regexp.MatchString(filePattern, item.Path); matched {
						finding := Finding{
							Repository: item.Repo.FullName,
							FilePath:   item.Path,
							URL:        item.HTMLURL,
							Pattern:    pattern,
							Severity:   determineSeverity(pattern),
						}
						allFindings = append(allFindings, finding)
						fmt.Printf("Found: %s in %s\n", item.Path, item.Repo.FullName)
						break
					}
				}
			}

			if len(result.Items) < perPage {
				break
			}

			page++
			time.Sleep(time.Duration(config.RateLimit) * time.Second)
		}
	}

	return allFindings, nil
}

func determineSeverity(pattern string) string {
	highSeverityPatterns := []string{
		"password",
		"secret",
		"key",
		"token",
		"credential",
	}

	for _, p := range highSeverityPatterns {
		if matched, _ := regexp.MatchString(p, pattern); matched {
			return "HIGH"
		}
	}
	return "MEDIUM"
}

func saveFindings(findings []Finding, outputFormat string) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(findings, "", "  ")
		if err != nil {
			return fmt.Errorf("error marshaling findings: %v", err)
		}
		return ioutil.WriteFile("findings.json", data, 0644)
	case "csv":
		file, err := os.Create("findings.csv")
		if err != nil {
			return fmt.Errorf("error creating CSV file: %v", err)
		}
		defer file.Close()

		file.WriteString("Repository,FilePath,URL,Pattern,Severity\n")
		for _, f := range findings {
			file.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n",
				f.Repository, f.FilePath, f.URL, f.Pattern, f.Severity))
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

func main() {
	fmt.Println("GitHub Security Scanner Demo")
	fmt.Println("===========================")
	fmt.Println("This demo will run for 60 seconds and show potential security issues found in public repositories.")
	fmt.Println("Note: This is a simplified demo version for learning purposes.")
	fmt.Println()

	configPath := flag.String("config", "config.json", "Path to configuration file")
	outputFormat := flag.String("output", "json", "Output format (json or csv)")
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create a context with 60-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stats := &RequestStats{}
	var allFindings []Finding
	for _, pattern := range config.SearchPatterns {
		select {
		case <-ctx.Done():
			break
		default:
			fmt.Printf("\nSearching for: %s\n", pattern)
			findings, err := searchGitHub(ctx, config, pattern, stats)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			allFindings = append(allFindings, findings...)
		}
	}

	if err := saveFindings(allFindings, *outputFormat); err != nil {
		fmt.Printf("Error saving findings: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nDemo complete! Found %d potential security issues.\n", len(allFindings))
	fmt.Printf("\nAPI Request Statistics:\n")
	fmt.Printf("Total Requests: %d\n", stats.TotalRequests)
	fmt.Printf("Successful Requests: %d\n", stats.SuccessfulRequests)
	fmt.Printf("Failed Requests: %d\n", stats.FailedRequests)
	fmt.Printf("Rate Limit Hits: %d\n", stats.RateLimitHits)
	fmt.Println("\nResults have been saved to findings.json")
	fmt.Println("\nTo run a full scan, remove the timeout and adjust the configuration.")
}
